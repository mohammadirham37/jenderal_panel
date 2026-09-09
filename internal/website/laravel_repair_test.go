package website

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestLaravelRepairInstallsMissingSQLiteExtensionBeforeMigration(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "repair-sqlite", "sqlite.example.com", "laravel", "8.4", "active")
	if _, err := db.Exec(`UPDATE websites SET framework='laravel',framework_version='13',frontend_stack='blade',setup_mode='auto-install',document_root='/home/web_sqlite_example_com/app/public' WHERE id='repair-sqlite'`); err != nil {
		t.Fatal(err)
	}

	installed := false
	restarted := false
	migrated := false
	var logs strings.Builder
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case name == "apt-get":
			if !strings.Contains(joined, "php8.4-sqlite3") {
				t.Fatalf("repair installed wrong package: %s", joined)
			}
			installed = true
			return &executor.Result{}, nil
		case name == "systemctl" && strings.Contains(joined, "restart php8.4-fpm"):
			restarted = true
			return &executor.Result{}, nil
		case strings.Contains(joined, `cat -- "$root/.env"`):
			return &executor.Result{Stdout: "APP_KEY=base64:keep\nDB_CONNECTION=sqlite\nDB_DATABASE=\"/home/web_sqlite_example_com/app/database/database.sqlite\"\n"}, nil
		case strings.Contains(joined, "extension_loaded('pdo_sqlite')"):
			if installed {
				return &executor.Result{}, nil
			}
			return &executor.Result{ExitCode: 42}, nil
		case strings.Contains(joined, " migrate "):
			migrated = true
			return &executor.Result{}, nil
		default:
			return &executor.Result{}, nil
		}
	}}

	svc := NewService(db, mock, nil)
	if err := svc.RepairLaravel(context.Background(), "repair-sqlite", func(line string) { logs.WriteString(line) }); err != nil {
		t.Fatal(err)
	}
	if !installed || !restarted || !migrated {
		t.Fatalf("repair result: installed=%v restarted=%v migrated=%v", installed, restarted, migrated)
	}
	if !strings.Contains(logs.String(), "Installing php8.4-sqlite3") {
		t.Fatalf("repair progress omitted SQLite installation: %s", logs.String())
	}
}

func TestLaravelRepairRequiresExplicitConfirmation(t *testing.T) {
	h := NewHandler(nil, nil)
	r := httptest.NewRecorder()
	h.RepairLaravel(r, httptest.NewRequest(http.MethodPost, "/api/v1/websites/example/repair-laravel", strings.NewReader(`{"confirm":false}`)))
	if r.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", r.Code)
	}
}

func TestLaravelRepairPreservesWebsiteAndSSLState(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "repair-example", "example.com", "laravel", "8.3", "active")
	_, err := db.Exec(`UPDATE websites SET framework='laravel',framework_version='12',frontend_stack='blade',setup_mode='auto-install',document_root='/home/web_example_com/app/public',ssl_enabled=1 WHERE id='repair-example'`)
	if err != nil {
		t.Fatal(err)
	}
	commands := 0
	mock := &executor.MockExecutor{RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		commands++
		if name != "-u" || args[0] != "web_example_com" {
			t.Fatalf("repair attempted privileged configuration mutation: %s %q", name, args)
		}
		return &executor.Result{Stdout: "APP_KEY=base64:keep\nDB_CONNECTION=mysql\nDB_DATABASE=production\n"}, nil
	}}
	svc := NewService(db, mock, nil)
	if err := svc.RepairLaravel(context.Background(), "repair-example", func(string) {}); err != nil {
		t.Fatal(err)
	}
	w, err := svc.Get(context.Background(), "repair-example")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "active" || !w.SSLEnabled || commands != 1 {
		t.Fatal("repair changed website state or ran external database commands")
	}
	if _, err := db.Exec(`UPDATE websites SET setup_mode='config-only' WHERE id='repair-example'`); err != nil {
		t.Fatal(err)
	}
	if err := svc.RepairLaravel(context.Background(), "repair-example", func(string) {}); err == nil {
		t.Fatal("configuration-only website accepted for automatic repair")
	}
}
