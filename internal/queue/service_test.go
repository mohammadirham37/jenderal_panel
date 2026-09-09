package queue

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
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
		id, id+".example.com", "php", "/home/"+webUser+"/public", webUser, "active", 0,
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

func TestListByWebsiteValidatesWebsiteAndScopesResults(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-a", "user-a")
	insertTestWebsite(t, db, "site-b", "user-b")
	insertTestWebsite(t, db, "site-empty", "user-empty")
	_, err := db.Exec(`INSERT INTO queue_workers (id, website_id, command, num_workers, auto_restart, status, created_at, updated_at)
		VALUES ('queue-a', 'site-a', 'cmd-a', 1, 1, 'running', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		       ('queue-b', 'site-b', 'cmd-b', 1, 1, 'running', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert queue workers: %v", err)
	}
	svc := NewService(db, mockExecutor(), nil)
	workers, err := svc.ListByWebsite(context.Background(), "site-a")
	if err != nil {
		t.Fatalf("ListByWebsite: %v", err)
	}
	if len(workers) != 1 || workers[0].ID != "queue-a" {
		t.Fatalf("workers = %#v, want only queue-a", workers)
	}
	emptyWorkers, err := svc.ListByWebsite(context.Background(), "site-empty")
	if err != nil || len(emptyWorkers) != 0 {
		t.Fatalf("empty website workers = %#v, error = %v, want empty result", emptyWorkers, err)
	}
	_, err = svc.ListByWebsite(context.Background(), "missing-site")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("error = %v, want model.ErrNotFound", err)
	}
}
