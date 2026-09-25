package website

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
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
