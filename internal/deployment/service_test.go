package deployment

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
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

// insertWebsite creates a test website record and returns its ID.
func insertWebsite(t *testing.T, db *sql.DB, domain, webUser, docRoot string) string {
	t.Helper()
	id := ulid.Make().String()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, document_root, web_user, status, created_at, updated_at)
		 VALUES (?, ?, 'php', ?, ?, 'active', ?, ?)`,
		id, domain, docRoot, webUser, now, now,
	)
	if err != nil {
		t.Fatalf("insert website: %v", err)
	}
	return id
}

func newMockExecutor() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func TestDeploy_CreatesRecord(t *testing.T) {
	db := setupTestDB(t)
	auditSvc := audit.NewService(db)
	exec := newMockExecutor()
	svc := NewService(db, exec, auditSvc)

	websiteID := insertWebsite(t, db, "example.com", "www-data", "/var/www/example")

	d, err := svc.Deploy(context.Background(), websiteID, "https://github.com/example/repo.git", "main")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if d.ID == "" {
		t.Error("expected non-empty deployment ID")
	}
	if d.WebsiteID != websiteID {
		t.Errorf("expected website_id %s, got %s", websiteID, d.WebsiteID)
	}
	if d.Status != "pending" {
		t.Errorf("expected status pending, got %s", d.Status)
	}
	if d.Branch != "main" {
		t.Errorf("expected branch main, got %s", d.Branch)
	}

	// Verify record exists in DB.
	got, err := svc.GetDeployment(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if got.Status != "pending" {
		t.Errorf("expected DB status pending, got %s", got.Status)
	}
}

func TestListByWebsite(t *testing.T) {
	db := setupTestDB(t)
	auditSvc := audit.NewService(db)
	exec := newMockExecutor()
	svc := NewService(db, exec, auditSvc)

	websiteID := insertWebsite(t, db, "test.com", "testuser", "/var/www/test")

	// Insert 2 deployments manually with different timestamps.
	id1 := ulid.Make().String()
	id2 := ulid.Make().String()
	t1 := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	t2 := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT INTO deployments (id, website_id, branch, status, created_at, updated_at)
		 VALUES (?, ?, 'main', 'success', ?, ?)`,
		id1, websiteID, t1, t1,
	)
	if err != nil {
		t.Fatalf("insert deployment 1: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO deployments (id, website_id, branch, status, created_at, updated_at)
		 VALUES (?, ?, 'develop', 'failed', ?, ?)`,
		id2, websiteID, t2, t2,
	)
	if err != nil {
		t.Fatalf("insert deployment 2: %v", err)
	}

	list, err := svc.ListByWebsite(context.Background(), websiteID)
	if err != nil {
		t.Fatalf("ListByWebsite: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("expected 2 deployments, got %d", len(list))
	}

	// Newest first (id2 was created later).
	if list[0].ID != id2 {
		t.Errorf("expected first deployment to be %s (newest), got %s", id2, list[0].ID)
	}
	if list[1].ID != id1 {
		t.Errorf("expected second deployment to be %s (oldest), got %s", id1, list[1].ID)
	}
}
