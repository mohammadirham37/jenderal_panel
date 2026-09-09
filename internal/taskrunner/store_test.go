package taskrunner

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
)

func migratedTaskDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func waitForStoredTask(t *testing.T, runner *Runner, id, status string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := runner.Get(id)
		if ok && task.Status == status {
			return *task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := runner.Get(id)
	t.Fatalf("task %s did not reach %s: %#v", id, status, task)
	return Task{}
}

func TestPersistentRunnerRestoresCompletedOutput(t *testing.T) {
	db := migratedTaskDB(t)
	runner, err := NewPersistent(db)
	if err != nil {
		t.Fatal(err)
	}
	id := runner.RunFuncWithOptions(Options{
		Name: "scan", Module: "security", Timeout: time.Minute,
	}, func(_ context.Context, log func(string)) error {
		log("file 1")
		return nil
	})
	waitForStoredTask(t, runner, id, "completed")

	restarted, err := NewPersistent(db)
	if err != nil {
		t.Fatal(err)
	}
	task, ok := restarted.Get(id)
	if !ok || task.Module != "security" || task.Output != "file 1\n" {
		t.Fatalf("restored task = %#v", task)
	}
}

func TestPersistentRunnerMarksInterruptedWorkFailed(t *testing.T) {
	db := migratedTaskDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO background_tasks
		(id, name, module, status, output, error, started_at, updated_at)
		VALUES ('running', 'install', 'security', 'running', 'step 1', '', ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}

	runner, err := NewPersistent(db)
	if err != nil {
		t.Fatal(err)
	}
	task, ok := runner.Get("running")
	if !ok || task.Status != "failed" || !strings.Contains(task.Error, "panel restarted") {
		t.Fatalf("task = %#v", task)
	}
}

func TestPersistentRunnerBoundsRetainedOutput(t *testing.T) {
	db := migratedTaskDB(t)
	runner, err := NewPersistent(db)
	if err != nil {
		t.Fatal(err)
	}
	id := runner.RunFuncWithOptions(Options{Name: "large", Timeout: time.Minute}, func(_ context.Context, log func(string)) error {
		log(strings.Repeat("x", maxTaskOutputBytes+1024))
		return nil
	})
	task := waitForStoredTask(t, runner, id, "completed")
	if len(task.Output) > maxTaskOutputBytes || !strings.HasPrefix(task.Output, outputTruncatedMarker) {
		t.Fatalf("output length=%d prefix=%q", len(task.Output), task.Output[:min(32, len(task.Output))])
	}
}
