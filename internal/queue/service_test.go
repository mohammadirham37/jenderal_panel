package queue

import (
	"context"
	"database/sql"
	"testing"

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

// insertTestWebsite creates a minimal website record for foreign key constraints.
func insertTestWebsite(t *testing.T, db *sql.DB, id, webUser string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, document_root, web_user, status, ssl_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "example.com", "php", "/home/"+webUser+"/public", webUser, "active", 0,
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert test website: %v", err)
	}
}

func mockExecutor() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
	}
}

func TestCreate(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")
	svc := NewService(db, mockExecutor(), nil)

	worker, err := svc.Create(context.Background(), QueueWorkerRequest{
		WebsiteID:  "web-001",
		Command:    "/usr/bin/php artisan queue:work",
		NumWorkers: 2,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if worker.ID == "" {
		t.Error("expected non-empty ID")
	}
	if worker.WebsiteID != "web-001" {
		t.Errorf("expected website_id web-001, got %s", worker.WebsiteID)
	}
	if worker.Command != "/usr/bin/php artisan queue:work" {
		t.Errorf("unexpected command: %s", worker.Command)
	}
	if worker.NumWorkers != 2 {
		t.Errorf("expected num_workers=2, got %d", worker.NumWorkers)
	}

	// Verify record in DB. Status should be "running" since mock executor
	// succeeds for all systemd commands.
	got, err := svc.Get(context.Background(), worker.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Command != worker.Command {
		t.Errorf("DB command mismatch: %s vs %s", got.Command, worker.Command)
	}
	if got.Status != "running" {
		t.Errorf("expected status=running after create with mock, got %s", got.Status)
	}
}

func TestList(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")
	svc := NewService(db, mockExecutor(), nil)

	_, err := svc.Create(context.Background(), QueueWorkerRequest{
		WebsiteID:  "web-001",
		Command:    "worker1",
		NumWorkers: 1,
	})
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}

	_, err = svc.Create(context.Background(), QueueWorkerRequest{
		WebsiteID:  "web-001",
		Command:    "worker2",
		NumWorkers: 1,
	})
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	workers, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(workers) != 2 {
		t.Errorf("expected 2 workers, got %d", len(workers))
	}
}
