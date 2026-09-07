package nodejs

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertTestWebsite creates a website record for foreign key references in tests.
func insertTestWebsite(t *testing.T, db *sql.DB, id, domain, webUser, docRoot string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, document_root, web_user, status, ssl_enabled, created_at, updated_at)
		 VALUES (?, ?, 'nodejs', ?, ?, 'active', 0, ?, ?)`,
		id, domain, docRoot, webUser, now, now,
	)
	if err != nil {
		t.Fatalf("insert test website: %v", err)
	}
}

// mockResult creates an executor.Result with the given stdout, stderr, and exit code.
func mockResult(stdout, stderr string, exitCode int) *executor.Result {
	return &executor.Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Duration: time.Millisecond,
	}
}

func newMockExec() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
	}
}

func TestCreateApp(t *testing.T) {
	db := setupTestDB(t)
	mock := newMockExec()

	websiteID := "site-001"
	insertTestWebsite(t, db, websiteID, "example.com", "exampleuser", "/home/exampleuser/public")

	svc := NewService(db, mock, nil)
	ctx := context.Background()

	app, err := svc.CreateApp(ctx, CreateAppRequest{
		WebsiteID:   websiteID,
		NodeVersion: "20",
		PackageMgr:  "npm",
		BuildCmd:    "npm run build",
		StartCmd:    "server.js",
		Port:        3000,
		EnvVars:     `{"DB_HOST":"localhost"}`,
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	if app.ID == "" {
		t.Error("expected non-empty ID")
	}
	if app.WebsiteID != websiteID {
		t.Errorf("expected website_id %q, got %q", websiteID, app.WebsiteID)
	}
	if app.NodeVersion != "20" {
		t.Errorf("expected node_version '20', got %q", app.NodeVersion)
	}
	if app.PackageMgr != "npm" {
		t.Errorf("expected package_mgr 'npm', got %q", app.PackageMgr)
	}
	if app.StartCmd != "server.js" {
		t.Errorf("expected start_cmd 'server.js', got %q", app.StartCmd)
	}
	if app.Port != 3000 {
		t.Errorf("expected port 3000, got %d", app.Port)
	}
	if app.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %q", app.Status)
	}

	// Verify DB record.
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != app.ID {
		t.Errorf("expected ID %q, got %q", app.ID, got.ID)
	}
	if got.BuildCmd != "npm run build" {
		t.Errorf("expected build_cmd 'npm run build', got %q", got.BuildCmd)
	}
	if got.EnvVars != `{"DB_HOST":"localhost"}` {
		t.Errorf("expected env_vars, got %q", got.EnvVars)
	}
}

func TestList(t *testing.T) {
	db := setupTestDB(t)
	mock := newMockExec()

	websiteID := "site-002"
	insertTestWebsite(t, db, websiteID, "test.com", "testuser", "/home/testuser/public")

	svc := NewService(db, mock, nil)
	ctx := context.Background()

	// Create 2 apps.
	_, err := svc.CreateApp(ctx, CreateAppRequest{
		WebsiteID:   websiteID,
		NodeVersion: "20",
		StartCmd:    "app1.js",
		Port:        3001,
	})
	if err != nil {
		t.Fatalf("CreateApp 1: %v", err)
	}

	_, err = svc.CreateApp(ctx, CreateAppRequest{
		WebsiteID:   websiteID,
		NodeVersion: "18",
		PackageMgr:  "yarn",
		StartCmd:    "start",
		Port:        3002,
	})
	if err != nil {
		t.Fatalf("CreateApp 2: %v", err)
	}

	apps, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}

	// Verify both apps are present (order may vary when created in same second).
	ports := map[int]bool{apps[0].Port: true, apps[1].Port: true}
	if !ports[3001] || !ports[3002] {
		t.Errorf("expected ports 3001 and 3002, got %d and %d", apps[0].Port, apps[1].Port)
	}

	// Verify ListByWebsite returns the same.
	byWebsite, err := svc.ListByWebsite(ctx, websiteID)
	if err != nil {
		t.Fatalf("ListByWebsite: %v", err)
	}
	if len(byWebsite) != 2 {
		t.Errorf("expected 2 apps by website, got %d", len(byWebsite))
	}
}

func TestCreateApp_Validation(t *testing.T) {
	db := setupTestDB(t)
	mock := newMockExec()
	svc := NewService(db, mock, nil)
	ctx := context.Background()

	// Missing website_id.
	_, err := svc.CreateApp(ctx, CreateAppRequest{
		StartCmd: "server.js",
		Port:     3000,
	})
	if err == nil {
		t.Error("expected error for missing website_id")
	}

	// Missing start_cmd.
	_, err = svc.CreateApp(ctx, CreateAppRequest{
		WebsiteID: "site-x",
		Port:      3000,
	})
	if err == nil {
		t.Error("expected error for missing start_cmd")
	}

	// Invalid port.
	_, err = svc.CreateApp(ctx, CreateAppRequest{
		WebsiteID: "site-x",
		StartCmd:  "server.js",
		Port:      0,
	})
	if err == nil {
		t.Error("expected error for invalid port")
	}

	// Invalid package manager.
	_, err = svc.CreateApp(ctx, CreateAppRequest{
		WebsiteID:  "site-x",
		StartCmd:   "server.js",
		Port:       3000,
		PackageMgr: "invalid",
	})
	if err == nil {
		t.Error("expected error for invalid package_mgr")
	}
}

func TestDelete(t *testing.T) {
	db := setupTestDB(t)
	mock := newMockExec()

	websiteID := "site-003"
	insertTestWebsite(t, db, websiteID, "del.com", "deluser", "/home/deluser/public")

	svc := NewService(db, mock, nil)
	ctx := context.Background()

	app, err := svc.CreateApp(ctx, CreateAppRequest{
		WebsiteID:   websiteID,
		NodeVersion: "20",
		StartCmd:    "server.js",
		Port:        4000,
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	if err := svc.Delete(ctx, app.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify it's gone.
	_, err = svc.Get(ctx, app.ID)
	if err == nil {
		t.Error("expected NOT_FOUND after delete")
	}
}

func TestListVersions(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "node" {
				return mockResult("v20.11.0\n", "", 0), nil
			}
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
	}

	svc := NewService(nil, mock, nil)
	versions, err := svc.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	if versions[0] != "20.11.0" {
		t.Errorf("expected '20.11.0', got %q", versions[0])
	}
}

func TestListVersions_NotInstalled(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "node: command not found", 127), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
	}

	svc := NewService(nil, mock, nil)
	versions, err := svc.ListVersions(context.Background())
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected 0 versions when not installed, got %d", len(versions))
	}
}
