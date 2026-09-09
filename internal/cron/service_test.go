package cron

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

	job, err := svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001",
		Command:   "/usr/bin/php artisan schedule:run",
		Schedule:  "* * * * *",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if job.ID == "" {
		t.Error("expected non-empty ID")
	}
	if job.WebsiteID != "web-001" {
		t.Errorf("expected website_id web-001, got %s", job.WebsiteID)
	}
	if job.Command != "/usr/bin/php artisan schedule:run" {
		t.Errorf("unexpected command: %s", job.Command)
	}
	if job.Schedule != "* * * * *" {
		t.Errorf("unexpected schedule: %s", job.Schedule)
	}
	if !job.Enabled {
		t.Error("expected enabled=true by default")
	}

	// Verify record in DB.
	got, err := svc.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Command != job.Command {
		t.Errorf("DB command mismatch: %s vs %s", got.Command, job.Command)
	}
}

func TestEnableDisable(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")
	svc := NewService(db, mockExecutor(), nil)

	job, err := svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001",
		Command:   "echo hello",
		Schedule:  "0 * * * *",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Disable.
	if err := svc.Disable(context.Background(), job.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	got, err := svc.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get after disable: %v", err)
	}
	if got.Enabled {
		t.Error("expected enabled=false after disable")
	}

	// Re-enable.
	if err := svc.Enable(context.Background(), job.ID); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	got, err = svc.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get after enable: %v", err)
	}
	if !got.Enabled {
		t.Error("expected enabled=true after enable")
	}
}

func TestList(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")
	svc := NewService(db, mockExecutor(), nil)

	_, err := svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001",
		Command:   "cmd1",
		Schedule:  "0 0 * * *",
	})
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}

	_, err = svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001",
		Command:   "cmd2",
		Schedule:  "0 12 * * *",
	})
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}

	jobs, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestListByWebsiteValidatesWebsiteAndScopesResults(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-a", "user-a")
	insertTestWebsite(t, db, "site-b", "user-b")
	insertTestWebsite(t, db, "site-empty", "user-empty")
	_, err := db.Exec(`INSERT INTO cron_jobs (id, website_id, command, schedule, enabled, created_at, updated_at)
		VALUES ('cron-a', 'site-a', 'cmd-a', '* * * * *', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		       ('cron-b', 'site-b', 'cmd-b', '* * * * *', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert cron jobs: %v", err)
	}
	svc := NewService(db, mockExecutor(), nil)
	jobs, err := svc.ListByWebsite(context.Background(), "site-a")
	if err != nil {
		t.Fatalf("ListByWebsite: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "cron-a" {
		t.Fatalf("jobs = %#v, want only cron-a", jobs)
	}
	emptyJobs, err := svc.ListByWebsite(context.Background(), "site-empty")
	if err != nil || len(emptyJobs) != 0 {
		t.Fatalf("empty website jobs = %#v, error = %v, want empty result", emptyJobs, err)
	}
	_, err = svc.ListByWebsite(context.Background(), "missing-site")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("error = %v, want model.ErrNotFound", err)
	}
}
