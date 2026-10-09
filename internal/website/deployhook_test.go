package website

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func insertAppWebsite(t *testing.T, db *sql.DB, domain, appType, runtime string, port int) string {
	t.Helper()
	webUser := DomainToUser(domain)
	docroot := "/home/" + webUser + "/app/public"
	_, err := db.Exec(`INSERT INTO websites (id, domain, app_type, app_runtime, app_port, app_start_command, app_build_command,
		document_root, web_user, status, framework, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, './app', 'go build -o app .', ?, ?, 'active', 'go', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		"w-"+domain, domain, appType, runtime, port, docroot, webUser)
	if err != nil {
		t.Fatal(err)
	}
	return "w-" + domain
}

func deployHookExecutor(t *testing.T, buildExit int, buildStderr string, isActive string) (*executor.MockExecutor, *[][]string) {
	t.Helper()
	var sudoCalls [][]string
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// Toolchain probe reports present; the siteops test -d probe
			// reports the new layout (no legacy .git in public).
			if name == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0, Stdout: "go version go1.27.1 linux/amd64"}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudoCalls = append(sudoCalls, append([]string{name}, args...))
			if name == "test" {
				// The legacy-layout probe must miss so the new <home>/app
				// layout is used; the go.mod check must hit.
				if strings.Contains(strings.Join(args, " "), "/public/.git") {
					return &executor.Result{ExitCode: 1}, nil
				}
				return &executor.Result{ExitCode: 0}, nil
			}
			if name == "systemctl" && len(args) >= 2 && args[0] == "is-active" {
				return &executor.Result{ExitCode: exitForActive(isActive), Stdout: isActive}, nil
			}
			if name == "systemctl" {
				return &executor.Result{ExitCode: 0}, nil
			}
			if name == "-u" {
				return &executor.Result{ExitCode: buildExit, Stderr: buildStderr}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}, &sudoCalls
}

func exitForActive(state string) int {
	if state == "active" {
		return 0
	}
	return 3
}

func hasSystemctlAction(calls *[][]string, unitAction string) bool {
	for _, c := range *calls {
		if len(c) >= 3 && c[0] == "systemctl" && c[1] == unitAction {
			return true
		}
	}
	return false
}

func TestRunDeployHook_NonGoIsNoOp(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock, sudoCalls := deployHookExecutor(t, 0, "", "active")
	svc := NewService(db, mock, nil)
	insertAppWebsite(t, db, "php.example.com", "php", "", 0)
	if err := svc.RunDeployHook(context.Background(), "w-php.example.com", func(string) {}); err != nil {
		t.Fatalf("RunDeployHook error = %v, want nil no-op", err)
	}
	if len(*sudoCalls) != 0 {
		t.Fatalf("sudo calls = %#v, want none for php site", *sudoCalls)
	}
}

func TestRunDeployHook_GoBuildsRestartsAndHealthChecks(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	mock, sudoCalls := deployHookExecutor(t, 0, "", "active")
	svc := NewService(db, mock, nil)
	insertAppWebsite(t, db, "go.example.com", "go", "go", port)
	var logged []string
	if err := svc.RunDeployHook(context.Background(), "w-go.example.com", func(l string) { logged = append(logged, l) }); err != nil {
		t.Fatalf("RunDeployHook error = %v", err)
	}
	if !hasSystemctlAction(sudoCalls, "restart") {
		t.Fatalf("restart not called; calls = %#v", *sudoCalls)
	}
	joined := strings.Join(logged, "\n")
	if !strings.Contains(joined, "app healthy on port") {
		t.Errorf("log missing healthy line: %v", logged)
	}
}

func TestRunDeployHook_BuildFailure_DoesNotRestart(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock, sudoCalls := deployHookExecutor(t, 1, "cannot find package", "active")
	svc := NewService(db, mock, nil)
	insertAppWebsite(t, db, "go.example.com", "go", "go", 8210)
	err := svc.RunDeployHook(context.Background(), "w-go.example.com", func(string) {})
	if err == nil || !strings.Contains(err.Error(), "cannot find package") {
		t.Fatalf("err = %v, want build failure surfaced", err)
	}
	if hasSystemctlAction(sudoCalls, "restart") || hasSystemctlAction(sudoCalls, "start") {
		t.Fatalf("unit restarted despite build failure; calls = %#v", *sudoCalls)
	}
}

func TestRunDeployHook_NoPort_ValidationError(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mock, _ := deployHookExecutor(t, 0, "", "active")
	svc := NewService(db, mock, nil)
	insertAppWebsite(t, db, "go.example.com", "go", "go", 0)
	err := svc.RunDeployHook(context.Background(), "w-go.example.com", func(string) {})
	var domainErr *model.DomainError
	if err == nil {
		t.Fatal("err = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "app port") {
		t.Fatalf("err = %v, want app port validation", err)
	}
	_ = domainErr
}

func TestRunDeployHook_GoBinary_SkipsBuildRestarts(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	mock, sudoCalls := deployHookExecutor(t, 0, "", "inactive")
	svc := NewService(db, mock, nil)
	insertAppWebsite(t, db, "go.example.com", "go", "go-binary", port)
	var logged []string
	if err := svc.RunDeployHook(context.Background(), "w-go.example.com", func(l string) { logged = append(logged, l) }); err != nil {
		t.Fatalf("RunDeployHook error = %v", err)
	}
	for _, c := range *sudoCalls {
		if c[0] == "-u" {
			t.Fatalf("build ran for go-binary site; calls = %#v", *sudoCalls)
		}
	}
	if !hasSystemctlAction(sudoCalls, "start") {
		t.Fatalf("start not called; calls = %#v", *sudoCalls)
	}
}

func TestRunDeployHook_HealsEmptyAppRuntime(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	mock, sudoCalls := deployHookExecutor(t, 0, "", "active")
	svc := NewService(db, mock, nil)
	// Legacy row: app_type go but app_runtime never backfilled.
	insertAppWebsite(t, db, "legacy.example.com", "go", "", port)
	if _, err := db.Exec(`UPDATE websites SET app_runtime = '' WHERE id = 'w-legacy.example.com'`); err != nil {
		t.Fatal(err)
	}
	var logged []string
	if err := svc.RunDeployHook(context.Background(), "w-legacy.example.com", func(l string) { logged = append(logged, l) }); err != nil {
		t.Fatalf("RunDeployHook error = %v (legacy go site must build via healed runtime)", err)
	}
	if !hasSystemctlAction(sudoCalls, "restart") {
		t.Fatalf("restart not called; calls = %#v", *sudoCalls)
	}
}

func TestDeployGoUpload_ExtractsToProjectRootAndRunsHook(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	mock, sudoCalls := deployHookExecutor(t, 0, "", "inactive")
	svc := NewService(db, mock, nil)
	insertAppWebsite(t, db, "go.example.com", "go", "go-binary", port)

	var logged []string
	if err := svc.deployGoUpload(context.Background(), mustGetWebsite(t, svc, "w-go.example.com"), "/tmp/upload.zip", "zip", func(l string) { logged = append(logged, l) }); err != nil {
		t.Fatalf("deployGoUpload error = %v", err)
	}
	var callLines []string
	for _, c := range *sudoCalls {
		callLines = append(callLines, strings.Join(c, " "))
	}
	joined := strings.Join(callLines, "\n")
	if !strings.Contains(joined, "/home/web_go_example_com/app") {
		t.Errorf("extraction did not target the project root: %s", joined)
	}
	if strings.Contains(joined, "-d /home/web_go_example_com/app/public") {
		t.Errorf("extraction targeted the document root: %s", joined)
	}
	if !hasSystemctlAction(sudoCalls, "start") {
		t.Errorf("deploy hook did not run after extraction: %s", joined)
	}
	_ = logged
}

func mustGetWebsite(t *testing.T, svc *Service, id string) model.Website {
	t.Helper()
	w, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
