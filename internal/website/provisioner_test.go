package website

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// setupTestDB creates an in-memory SQLite database with the websites and domains tables.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// Enable foreign keys.
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	if err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS websites (
		id            TEXT PRIMARY KEY,
		domain        TEXT NOT NULL UNIQUE,
		app_type      TEXT NOT NULL DEFAULT 'php',
		php_version   TEXT,
		document_root TEXT NOT NULL,
		web_user      TEXT NOT NULL UNIQUE,
		status        TEXT NOT NULL DEFAULT 'pending',
		error_message TEXT,
		ssl_enabled   INTEGER NOT NULL DEFAULT 0,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS domains (
		id         TEXT PRIMARY KEY,
		website_id TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
		name       TEXT NOT NULL UNIQUE,
		type       TEXT NOT NULL DEFAULT 'alias',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS audit_logs (
		id         TEXT PRIMARY KEY,
		user_id    TEXT,
		action     TEXT NOT NULL,
		module     TEXT NOT NULL,
		target     TEXT,
		detail     TEXT,
		ip_address TEXT,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS ssl_certificates (
		id            TEXT PRIMARY KEY,
		website_id    TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
		domain        TEXT NOT NULL,
		issuer        TEXT NOT NULL DEFAULT 'letsencrypt',
		status        TEXT NOT NULL DEFAULT 'pending',
		expires_at    TEXT,
		auto_renew    INTEGER NOT NULL DEFAULT 1,
		error_message TEXT,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	return db
}

// insertTestWebsite inserts a website and its primary domain into the test database.
func insertTestWebsite(t *testing.T, db *sql.DB, id, domain, appType, phpVersion, status string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	webUser := DomainToUser(domain)
	docRoot := "/home/" + webUser + "/public"

	_, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, ssl_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		id, domain, appType, phpVersion, docRoot, webUser, status, now, now,
	)
	if err != nil {
		t.Fatalf("insert website: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO domains (id, website_id, name, type, created_at) VALUES (?, ?, ?, 'primary', ?)`,
		"dom-"+id, id, domain, now,
	)
	if err != nil {
		t.Fatalf("insert domain: %v", err)
	}
}

// getWebsiteStatus reads the current status and error_message from the DB.
func getWebsiteStatus(t *testing.T, db *sql.DB, id string) (string, string) {
	t.Helper()
	var status string
	var errMsg sql.NullString
	err := db.QueryRow(`SELECT status, error_message FROM websites WHERE id = ?`, id).Scan(&status, &errMsg)
	if err != nil {
		t.Fatalf("get website status: %v", err)
	}
	return status, errMsg.String
}

func TestProvision_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-001", "example.com", "php", "8.2", "pending")

	// Track status transitions.
	var mu sync.Mutex
	var statuses []string

	origUpdateStatus := func(ctx context.Context, websiteID, status, errorMessage string) {
		mu.Lock()
		statuses = append(statuses, status)
		mu.Unlock()
	}

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
	}

	prov := NewProvisioner(db, mock, nil)

	// Wrap provision to track status transitions.
	// We call provision directly (synchronous) for testing.
	origProvision := prov.provision

	// Override updateStatus to track transitions.
	_ = origUpdateStatus
	_ = origProvision

	// Just call provision directly.
	prov.provision(context.Background(), "ws-001")

	// Verify final status is active.
	status, errMsg := getWebsiteStatus(t, db, "ws-001")
	if status != "active" {
		t.Errorf("expected status 'active', got %q (error_message: %q)", status, errMsg)
	}
	if errMsg != "" {
		t.Errorf("expected empty error_message, got %q", errMsg)
	}
}

func TestProvisionCreatesDefaultWebsiteFiles(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-defaults", "coming-soon.example.com", "php", "8.2", "pending")

	written := make(map[string]string)
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "-u" && len(args) >= 5 && args[2] == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(_ context.Context, input, name string, args ...string) (*executor.Result, error) {
			if name != "-u" || len(args) < 5 || args[1] != "--" || args[2] != "tee" {
				t.Fatalf("unexpected default-file command %q %q", name, args)
			}
			written[args[len(args)-1]] = input
			return &executor.Result{ExitCode: 0}, nil
		},
	}

	NewProvisioner(db, mock, nil).provision(context.Background(), "ws-defaults")
	docRoot := "/home/web_coming_soon_example_com/public"
	index := written[docRoot+"/index.html"]
	if !strings.Contains(index, "coming-soon.example.com") || !strings.Contains(index, "Website sedang dikembangkan") {
		t.Fatalf("default index.html does not contain the domain and development message:\n%s", index)
	}
	if !strings.Contains(index, "cdn.tailwindcss.com") || !strings.Contains(index, "Jenderal-Panel") {
		t.Fatalf("default index.html is missing Tailwind/Jenderal-Panel branding:\n%s", index)
	}
	if got := written[docRoot+"/robots.txt"]; got != "User-agent: *\nDisallow: /\n" {
		t.Fatalf("robots.txt = %q, want crawler blocking defaults", got)
	}
}

func TestProvisionDoesNotOverwriteExistingDefaultWebsiteFiles(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-existing", "existing.example.com", "static", "", "pending")

	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "-u" && len(args) >= 5 && args[2] == "test" {
				return &executor.Result{ExitCode: 0}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
			t.Fatal("existing website files must not be overwritten")
			return nil, nil
		},
	}

	NewProvisioner(db, mock, nil).provision(context.Background(), "ws-existing")
	status, errMsg := getWebsiteStatus(t, db, "ws-existing")
	if status != "active" {
		t.Fatalf("website status = %q, want active; error = %q", status, errMsg)
	}
}

func TestProvisionUsesDetectedIPv6Capability(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-ipv6", "ipv6.example.com", "static", "", "pending")

	var vhost string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "cp" && len(args) == 2 && args[1] == "/etc/nginx/sites-available/ipv6.example.com" {
				content, err := os.ReadFile(args[0])
				if err != nil {
					t.Fatalf("read rendered vhost: %v", err)
				}
				vhost = string(content)
			}
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
	}

	prov := NewProvisioner(db, mock, nil)
	prov.ipv6Available = func() bool { return true }
	prov.provision(context.Background(), "ws-ipv6")

	if !strings.Contains(vhost, "listen [::]:80;") {
		t.Fatalf("provisioned vhost did not use detected IPv6 support:\n%s", vhost)
	}
}

func TestProvision_NginxFail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-002", "fail.example.com", "php", "8.2", "pending")

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// Make nginx -t fail.
			if name == "nginx" && len(args) > 0 && args[0] == "-t" {
				return &executor.Result{
					ExitCode: 1,
					Stderr:   "nginx: configuration file test failed",
					Duration: time.Millisecond,
				}, nil
			}
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
	}

	prov := NewProvisioner(db, mock, nil)
	prov.provision(context.Background(), "ws-002")

	status, errMsg := getWebsiteStatus(t, db, "ws-002")
	if status != "failed" {
		t.Errorf("expected status 'failed', got %q", status)
	}
	if !strings.Contains(errMsg, "nginx config test failed") {
		t.Errorf("expected error_message to contain 'nginx config test failed', got %q", errMsg)
	}
}
