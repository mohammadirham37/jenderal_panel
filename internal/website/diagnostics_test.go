package website

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// setupDiagnosticsDB prepares the shared test schema with the serving
// diagnostics columns Service.Get reads. Column additions are guarded against
// pragma_table_info so this stays correct if the shared schema grows them.
func setupDiagnosticsDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)
	rows, err := db.Query(`SELECT name FROM pragma_table_info('websites')`)
	if err != nil {
		t.Fatalf("read websites schema: %v", err)
	}
	existing := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan column name: %v", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("read websites schema: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("read websites schema: %v", err)
	}
	for _, column := range []string{"proxy_scheme", "proxy_host", "proxy_port"} {
		if existing[column] {
			continue
		}
		definition := column + " TEXT NOT NULL DEFAULT ''"
		if column == "proxy_port" {
			definition = column + " INTEGER NOT NULL DEFAULT 0"
		}
		if _, err := db.Exec(`ALTER TABLE websites ADD COLUMN ` + definition); err != nil {
			t.Fatalf("add column %s: %v", column, err)
		}
	}
	return db
}

func TestDiagnoseHealthyLaravelSite(t *testing.T) {
	db := setupDiagnosticsDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "diag-ok", "diag.example.com", "php", "8.3", "active")
	if _, err := db.Exec(`UPDATE websites SET framework='laravel', document_root='/home/web_diag_example_com/app/public' WHERE id='diag-ok'`); err != nil {
		t.Fatal(err)
	}

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			joined := name + " " + strings.Join(args, " ")
			switch {
			case joined == "cat /etc/nginx/nginx.conf":
				return &executor.Result{Stdout: "user www-data;\n"}, nil
			case name == "curl":
				return &executor.Result{Stdout: "200"}, nil
			case name == "systemctl":
				return &executor.Result{Stdout: "active"}, nil
			case name == "tail":
				return &executor.Result{Stdout: "2026/09/25 10:00:00 [error] sample line\n"}, nil
			default:
				// test probes (vhost, docroot, artisan, .env, symlink,
				// traversal) all succeed.
				return &executor.Result{ExitCode: 0}, nil
			}
		},
	}

	svc := NewService(db, mock, nil)
	report, err := svc.Diagnose(context.Background(), "diag-ok")
	if err != nil {
		t.Fatal(err)
	}
	if report.Overall != DiagOK {
		t.Fatalf("expected overall ok, got %s: %+v", report.Overall, report.Checks)
	}
	statuses := map[string]DiagnosticCheck{}
	for _, c := range report.Checks {
		statuses[c.ID] = c
	}
	for _, id := range []string{"vhost", "nginx_config", "docroot", "nginx_access", "http_response", "php_fpm", "laravel_project"} {
		c, ok := statuses[id]
		if !ok {
			t.Fatalf("missing check %s in %+v", id, report.Checks)
		}
		if c.Status != DiagOK {
			t.Fatalf("check %s = %s (%s)", id, c.Status, c.Detail)
		}
	}
	if !strings.Contains(report.ErrorLog, "sample line") {
		t.Fatalf("error log not included: %q", report.ErrorLog)
	}
}

func TestDiagnoseReportsNginxPermissionDenied(t *testing.T) {
	db := setupDiagnosticsDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "diag-deny", "diag2.example.com", "php", "8.3", "active")
	if _, err := db.Exec(`UPDATE websites SET framework='laravel', document_root='/home/web_diag2_example_com/app/public' WHERE id='diag-deny'`); err != nil {
		t.Fatal(err)
	}

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			joined := name + " " + strings.Join(args, " ")
			switch {
			case joined == "cat /etc/nginx/nginx.conf":
				return &executor.Result{Stdout: "user www-data;\n"}, nil
			case name == "curl":
				// nginx answers 404 "File not found".
				return &executor.Result{Stdout: "404"}, nil
			case name == "-u":
				// The worker account cannot enter the project directory.
				if strings.Contains(joined, "test -x /home/web_diag2_example_com/app") {
					return &executor.Result{ExitCode: 1}, nil
				}
				return &executor.Result{ExitCode: 0}, nil
			default:
				return &executor.Result{ExitCode: 0}, nil
			}
		},
	}

	svc := NewService(db, mock, nil)
	report, err := svc.Diagnose(context.Background(), "diag-deny")
	if err != nil {
		t.Fatal(err)
	}
	if report.Overall != DiagFail {
		t.Fatalf("expected overall fail, got %s: %+v", report.Overall, report.Checks)
	}
	var access, httpResponse *DiagnosticCheck
	for i := range report.Checks {
		switch report.Checks[i].ID {
		case "nginx_access":
			access = &report.Checks[i]
		case "http_response":
			httpResponse = &report.Checks[i]
		}
	}
	if access == nil || access.Status != DiagFail {
		t.Fatalf("expected nginx_access fail, got %+v", access)
	}
	if access.Hint != "wd.diag.hint_permissions" {
		t.Fatalf("expected permission hint, got %q", access.Hint)
	}
	if !strings.Contains(access.Detail, "permission denied") {
		t.Fatalf("expected permission detail, got %q", access.Detail)
	}
	if httpResponse == nil || httpResponse.Status != DiagFail {
		t.Fatalf("expected http_response fail, got %+v", httpResponse)
	}
}

func TestDiagnosticIndexFileForWordPress(t *testing.T) {
	cases := []struct {
		w    model.Website
		want string
	}{
		{model.Website{AppType: "wordpress", Framework: "wordpress", NginxProfile: "wordpress"}, "index.php"},
		{model.Website{AppType: "php"}, "index.php"},
		{model.Website{AppType: "php", Framework: "laravel"}, "index.php"},
		{model.Website{AppType: "static"}, "index.html"},
		{model.Website{AppType: "static", NginxProfile: "php"}, "index.php"},
	}
	for _, c := range cases {
		if got := diagnosticIndexFile(c.w); got != c.want {
			t.Errorf("diagnosticIndexFile(%+v) = %q, want %q", c.w, got, c.want)
		}
	}
}

func goDiagnosticsMock(vhost string, unitState string, curlCode string) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			joined := name + " " + strings.Join(args, " ")
			switch {
			case strings.HasPrefix(joined, "cat /etc/nginx/sites-enabled/"):
				return &executor.Result{Stdout: vhost}, nil
			case name == "curl":
				return &executor.Result{Stdout: curlCode}, nil
			case name == "systemctl" && len(args) > 0 && args[0] == "is-active":
				code := 0
				if unitState != "active" {
					code = 3
				}
				return &executor.Result{Stdout: unitState + "\n", ExitCode: code}, nil
			case name == "systemctl":
				return &executor.Result{Stdout: "ActiveState=" + unitState + "\nSubState=running\nMainPID=0\nNRestarts=0\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func TestDiagnoseGoSiteChecksAppNotFiles(t *testing.T) {
	db := setupDiagnosticsDB(t)
	defer db.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	id := insertAppWebsite(t, db, "go.example.com", "go", "go", port)
	if _, err := db.Exec(`UPDATE websites SET nginx_profile = 'app-proxy' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}

	vhost := "location / {\n    try_files $uri @app;\n}\nlocation @app {\n    proxy_pass http://127.0.0.1:" + strconv.Itoa(port) + ";\n}\n"
	svc := NewService(db, goDiagnosticsMock(vhost, "active", "200"), nil)
	report, err := svc.Diagnose(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]DiagnosticCheck{}
	for _, c := range report.Checks {
		got[c.ID] = c
	}
	for _, unwanted := range []string{"docroot", "nginx_access", "php_fpm"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("Go site must not run the %s file check: %+v", unwanted, got[unwanted])
		}
	}
	for _, id := range []string{"vhost", "nginx_config", "app_vhost", "app_service", "http_response"} {
		if got[id].Status != DiagOK {
			t.Errorf("check %s = %+v, want ok", id, got[id])
		}
	}
	if report.Overall != DiagOK {
		t.Errorf("overall = %s", report.Overall)
	}
}

func TestDiagnoseGoSiteFlagsStaleVhostAndDownApp(t *testing.T) {
	db := setupDiagnosticsDB(t)
	defer db.Close()
	// Port 1 is never listening in tests.
	id := insertAppWebsite(t, db, "go.example.com", "go", "go", 1)
	if _, err := db.Exec(`UPDATE websites SET nginx_profile = 'app-proxy' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, goDiagnosticsMock("location / {\n    try_files $uri $uri/ @app;\n}\n", "activating", "403"), nil)
	report, err := svc.Diagnose(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]DiagnosticCheck{}
	for _, c := range report.Checks {
		got[c.ID] = c
	}
	if got["app_vhost"].Status != DiagFail || got["app_vhost"].Hint != "wd.diag.hint_app_vhost" {
		t.Errorf("stale try_files not flagged: %+v", got["app_vhost"])
	}
	if got["app_service"].Status != DiagFail || got["app_service"].Hint != "wd.diag.hint_app_down" {
		t.Errorf("down app not flagged: %+v", got["app_service"])
	}
	if got["http_response"].Hint != "wd.diag.hint_app_vhost" {
		t.Errorf("a 403 on a Go site should point at the vhost, got %+v", got["http_response"])
	}
}

func TestRepairSitesContinuesPastFailures(t *testing.T) {
	var repaired []string
	var log []string
	sites := []repairSite{{"a", "a.test"}, {"b", "b.test"}, {"c", "c.test"}}
	err := repairSites(context.Background(), sites, func(_ context.Context, id string) error {
		repaired = append(repaired, id)
		if id == "b" {
			return errors.New("nginx -t failed")
		}
		return nil
	}, func(line string) { log = append(log, line) })
	if strings.Join(repaired, ",") != "a,b,c" {
		t.Errorf("repaired = %v, want every site attempted", repaired)
	}
	if err == nil || !strings.Contains(err.Error(), "b.test") || strings.Contains(err.Error(), "a.test") {
		t.Errorf("err = %v, want only b.test named", err)
	}
	if !strings.Contains(strings.Join(log, "\n"), "Done: 2 repaired, 1 failed.") {
		t.Errorf("summary missing: %v", log)
	}
}

func TestActiveSitesForRepairSkipsInactive(t *testing.T) {
	db := setupDiagnosticsDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "s1", "live.example.com", "php", "8.3", "active")
	insertTestWebsite(t, db, "s2", "off.example.com", "php", "8.3", "suspended")
	insertTestWebsite(t, db, "s3", "new.example.com", "php", "8.3", "pending")
	svc := NewService(db, &executor.MockExecutor{}, nil)
	sites, err := svc.activeSitesForRepair(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].domain != "live.example.com" {
		t.Errorf("sites = %+v, want only the active one", sites)
	}
}
