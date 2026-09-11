package backup

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

func TestCreateBackup(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	b, err := svc.CreateBackup(context.Background(), SystemCaller, "config", "")
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	if b.ID == "" {
		t.Error("expected non-empty ID")
	}
	if b.Type != "config" {
		t.Errorf("expected type config, got %s", b.Type)
	}
	if b.Status != "pending" {
		t.Errorf("expected status pending, got %s", b.Status)
	}
	if b.Storage != "local" {
		t.Errorf("expected storage local, got %s", b.Storage)
	}

	// Verify record exists in DB.
	got, err := svc.Get(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != b.ID {
		t.Errorf("DB ID mismatch: %s vs %s", got.ID, b.ID)
	}
	if got.Type != "config" {
		t.Errorf("DB type mismatch: %s", got.Type)
	}
}

func TestCreateBackupValidation(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	// Missing type.
	_, err := svc.CreateBackup(context.Background(), SystemCaller, "", "")
	if err == nil {
		t.Error("expected error for empty type")
	}

	// Invalid type.
	_, err = svc.CreateBackup(context.Background(), SystemCaller, "invalid", "")
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Insert two backups directly.
	_, err := db.Exec(
		`INSERT INTO backups (id, type, target, storage, path, size_bytes, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"b-001", "config", nil, "local", "/tmp/b1.tar.gz", 1024, "completed", nowStr,
	)
	if err != nil {
		t.Fatalf("insert backup 1: %v", err)
	}

	// Insert second backup 1 second later so ordering is deterministic.
	later := now.Add(1 * time.Second).Format(time.RFC3339)
	_, err = db.Exec(
		`INSERT INTO backups (id, type, target, storage, path, size_bytes, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"b-002", "website", "example.com", "local", "/tmp/b2.tar.gz", 2048, "completed", later,
	)
	if err != nil {
		t.Fatalf("insert backup 2: %v", err)
	}

	backups, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(backups))
	}

	// Should be ordered by created_at DESC, so b-002 first.
	if backups[0].ID != "b-002" {
		t.Errorf("expected first backup ID b-002, got %s", backups[0].ID)
	}
	if backups[1].ID != "b-001" {
		t.Errorf("expected second backup ID b-001, got %s", backups[1].ID)
	}
}

func TestCreateSchedule(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	sched, err := svc.CreateSchedule(context.Background(), SystemCaller, ScheduleRequest{
		Type:          "database",
		Target:        "mydb",
		Storage:       "local",
		Schedule:      "daily",
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	if sched.ID == "" {
		t.Error("expected non-empty ID")
	}
	if sched.Type != "database" {
		t.Errorf("expected type database, got %s", sched.Type)
	}
	if sched.Target != "mydb" {
		t.Errorf("expected target mydb, got %s", sched.Target)
	}
	if sched.Schedule != "daily" {
		t.Errorf("expected schedule daily, got %s", sched.Schedule)
	}
	if sched.RetentionDays != 14 {
		t.Errorf("expected retention_days 14, got %d", sched.RetentionDays)
	}
	if !sched.Enabled {
		t.Error("expected enabled=true by default")
	}

	// Verify record in DB.
	got, err := svc.GetSchedule(context.Background(), sched.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.Type != "database" {
		t.Errorf("DB type mismatch: %s", got.Type)
	}
}

func TestCreateScheduleValidation(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	// Missing type.
	_, err := svc.CreateSchedule(context.Background(), SystemCaller, ScheduleRequest{
		Schedule: "daily",
	})
	if err == nil {
		t.Error("expected error for empty type")
	}

	// Missing schedule.
	_, err = svc.CreateSchedule(context.Background(), SystemCaller, ScheduleRequest{
		Type: "config",
	})
	if err == nil {
		t.Error("expected error for empty schedule")
	}
}

func TestEnableDisableSchedule(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	sched, err := svc.CreateSchedule(context.Background(), SystemCaller, ScheduleRequest{
		Type:     "config",
		Schedule: "daily",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	// Disable.
	if err := svc.DisableSchedule(context.Background(), SystemCaller, sched.ID); err != nil {
		t.Fatalf("DisableSchedule: %v", err)
	}

	got, err := svc.GetSchedule(context.Background(), sched.ID)
	if err != nil {
		t.Fatalf("GetSchedule after disable: %v", err)
	}
	if got.Enabled {
		t.Error("expected enabled=false after disable")
	}

	// Re-enable.
	if err := svc.EnableSchedule(context.Background(), SystemCaller, sched.ID); err != nil {
		t.Fatalf("EnableSchedule: %v", err)
	}

	got, err = svc.GetSchedule(context.Background(), sched.ID)
	if err != nil {
		t.Fatalf("GetSchedule after enable: %v", err)
	}
	if !got.Enabled {
		t.Error("expected enabled=true after enable")
	}
}

func TestDeleteBackup(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO backups (id, type, storage, path, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"b-del", "config", "local", "/tmp/del.tar.gz", "completed", now,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := svc.DeleteBackup(context.Background(), "b-del"); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}

	_, err = svc.Get(context.Background(), "b-del")
	if err == nil {
		t.Error("expected not found after delete")
	}
}

func TestDeleteSchedule(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	sched, err := svc.CreateSchedule(context.Background(), SystemCaller, ScheduleRequest{
		Type:     "config",
		Schedule: "daily",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	if err := svc.DeleteSchedule(context.Background(), SystemCaller, sched.ID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}

	_, err = svc.GetSchedule(context.Background(), sched.ID)
	if err == nil {
		t.Error("expected not found after delete")
	}
}

func TestUpdateSchedule(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	sched, err := svc.CreateSchedule(context.Background(), SystemCaller, ScheduleRequest{
		Type:          "config",
		Schedule:      "daily",
		RetentionDays: 7,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	err = svc.UpdateSchedule(context.Background(), SystemCaller, sched.ID, ScheduleRequest{
		Type:          "website",
		Target:        "example.com",
		Schedule:      "weekly",
		RetentionDays: 30,
	})
	if err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}

	got, err := svc.GetSchedule(context.Background(), sched.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.Type != "website" {
		t.Errorf("expected type website, got %s", got.Type)
	}
	if got.Target != "example.com" {
		t.Errorf("expected target example.com, got %s", got.Target)
	}
	if got.Schedule != "weekly" {
		t.Errorf("expected schedule weekly, got %s", got.Schedule)
	}
	if got.RetentionDays != 30 {
		t.Errorf("expected retention_days 30, got %d", got.RetentionDays)
	}
}

func TestListSchedules(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, mockExecutor(), nil, "/tmp/test-backups")

	_, err := svc.CreateSchedule(context.Background(), SystemCaller, ScheduleRequest{
		Type:     "config",
		Schedule: "daily",
	})
	if err != nil {
		t.Fatalf("CreateSchedule 1: %v", err)
	}

	_, err = svc.CreateSchedule(context.Background(), SystemCaller, ScheduleRequest{
		Type:     "database",
		Target:   "mydb",
		Schedule: "weekly",
	})
	if err != nil {
		t.Fatalf("CreateSchedule 2: %v", err)
	}

	schedules, err := svc.ListSchedules(context.Background(), SystemCaller)
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(schedules) != 2 {
		t.Errorf("expected 2 schedules, got %d", len(schedules))
	}
}

func TestShouldRun(t *testing.T) {
	now := time.Now().UTC()

	// Never run before => should run.
	if !shouldRun("daily", time.Time{}, now) {
		t.Error("expected shouldRun=true for zero lastRun")
	}

	// Ran 25 hours ago with daily schedule => should run.
	if !shouldRun("daily", now.Add(-25*time.Hour), now) {
		t.Error("expected shouldRun=true for daily, last run 25h ago")
	}

	// Ran 1 hour ago with daily schedule => should not run.
	if shouldRun("daily", now.Add(-1*time.Hour), now) {
		t.Error("expected shouldRun=false for daily, last run 1h ago")
	}

	// Hourly schedule, ran 2 hours ago => should run.
	if !shouldRun("hourly", now.Add(-2*time.Hour), now) {
		t.Error("expected shouldRun=true for hourly, last run 2h ago")
	}
}

func TestGeneratePath(t *testing.T) {
	path := generatePath("/var/lib/jenderal/backups", "website", "example.com")
	if path == "" {
		t.Error("expected non-empty path")
	}
	if len(path) < 10 {
		t.Error("path seems too short")
	}
}
