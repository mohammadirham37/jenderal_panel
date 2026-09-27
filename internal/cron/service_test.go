package cron

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
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
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			return 0, nil
		},
	}
}

func insertCronJob(t *testing.T, db *sql.DB, id, websiteID, command, schedule string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO cron_jobs (id, website_id, command, schedule, enabled, last_run, last_status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		id, websiteID, command, schedule,
	)
	if err != nil {
		t.Fatalf("insert cron job: %v", err)
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

// A crontab entry is a single line; embedded line breaks would let one job
// inject additional entries into the site's crontab.
func TestValidateCronFieldsRejectsLineBreaks(t *testing.T) {
	if err := validateCronFields("*/5 * * * *", "echo ok"); err != nil {
		t.Errorf("expected a normal job to pass, got %v", err)
	}
	if err := validateCronFields("*/5 * * * *\n0 0 * * * curl evil", "echo ok"); err == nil {
		t.Error("expected a newline in schedule to be rejected")
	}
	if err := validateCronFields("*/5 * * * *", "echo ok\ncurl evil"); err == nil {
		t.Error("expected a newline in command to be rejected")
	}
	if err := validateCronFields("*/5 * * * *", "echo \r ok"); err == nil {
		t.Error("expected a carriage return in command to be rejected")
	}
	if err := validateCronFields("@daily", "backup.sh\x00x"); err == nil {
		t.Error("expected a NUL byte in command to be rejected")
	}
}

// waitForTask polls the task runner until the task leaves the running state.
func waitForTask(t *testing.T, tr *taskrunner.Runner, taskID string) *taskrunner.Task {
	t.Helper()
	for i := 0; i < 250; i++ {
		task, ok := tr.Get(taskID)
		if !ok {
			t.Fatalf("task %s not found", taskID)
		}
		if task.Status != "running" {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task did not finish in time")
	return nil
}

// TestRebuildCrontabWritesWrappersAndStreamsCrontab pins the wrapper-based
// crontab: every enabled job gets a wrapper script recording its runs, and
// the crontab itself is streamed through stdin — the old echo %q approach
// merged multi-job crontabs into one line so later jobs never ran.
func TestRebuildCrontabWritesWrappersAndStreamsCrontab(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")

	var wrappers []string
	var crontabInput string
	var crontabArgs []string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			wrappers = append(wrappers, name+" "+strings.Join(args, " ")+"\n"+input)
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			crontabArgs = append([]string{name}, args...)
			raw, readErr := io.ReadAll(stdin)
			if readErr != nil {
				return 1, readErr
			}
			crontabInput = string(raw)
			return 0, nil
		},
	}
	svc := NewService(db, mock, nil)

	if _, err := svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001", Command: "php artisan schedule:run", Schedule: "* * * * *",
	}); err != nil {
		t.Fatalf("Create job1: %v", err)
	}
	if _, err := svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001", Command: "/usr/bin/backup.sh", Schedule: "0 2 * * *",
	}); err != nil {
		t.Fatalf("Create job2: %v", err)
	}

	// A wrapper per job, carrying the command and the run bookkeeping.
	if len(wrappers) < 2 {
		t.Fatalf("wrapper scripts written = %d, want at least one per job", len(wrappers))
	}
	joinedWrappers := strings.Join(wrappers, "\n---\n")
	for _, want := range []string{"#!/bin/sh", "php artisan schedule:run", "/usr/bin/backup.sh", `.last"`, `.exit"`} {
		if !strings.Contains(joinedWrappers, want) {
			t.Errorf("wrappers missing %q:\n%s", want, joinedWrappers)
		}
	}

	// The crontab streams through crontab(1) with one wrapper invocation per
	// job — no echo, no escaped newlines.
	joinedArgs := strings.Join(crontabArgs, " ")
	if !strings.Contains(joinedArgs, "crontab -u testuser -") {
		t.Errorf("crontab args = %q", joinedArgs)
	}
	if strings.Count(crontabInput, "\n") != 2 || strings.Contains(crontabInput, "\\n") {
		t.Fatalf("crontab must be one line per job, got %q", crontabInput)
	}
	if !strings.Contains(crontabInput, "* * * * * /home/testuser/.jenderal-cron/") ||
		!strings.Contains(crontabInput, "0 2 * * * /home/testuser/.jenderal-cron/") ||
		!strings.Contains(crontabInput, "> /dev/null 2>&1") {
		t.Errorf("crontab lines = %q", crontabInput)
	}
}

// TestListByWebsiteOverlaysRecordedRuns makes the Last Run column reflect
// what the cron daemon actually executed.
func TestListByWebsiteOverlaysRecordedRuns(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")
	insertCronJob(t, db, "job-ok", "web-001", "ok command", "* * * * *")
	insertCronJob(t, db, "job-fail", "web-001", "failing command", "0 2 * * *")

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			joined := name + " " + strings.Join(args, " ")
			switch {
			case strings.HasPrefix(joined, "test -f "):
				return &executor.Result{ExitCode: 0}, nil // wrapper exists
			case strings.Contains(joined, "job-ok.last"):
				return &executor.Result{ExitCode: 0, Stdout: "2026-09-27T10:00:00Z\n"}, nil
			case strings.Contains(joined, "job-ok.exit"):
				return &executor.Result{ExitCode: 0, Stdout: "0\n"}, nil
			case strings.Contains(joined, "job-fail.last"):
				return &executor.Result{ExitCode: 0, Stdout: "2026-09-27T10:05:00Z\n"}, nil
			case strings.Contains(joined, "job-fail.exit"):
				return &executor.Result{ExitCode: 0, Stdout: "1\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil)

	jobs, err := svc.ListByWebsite(context.Background(), "web-001")
	if err != nil {
		t.Fatalf("ListByWebsite: %v", err)
	}

	byID := map[string]model.CronJob{}
	for _, job := range jobs {
		byID[job.ID] = job
	}
	okJob, ok := byID["job-ok"]
	if !ok || okJob.LastRun.IsZero() || okJob.LastStatus != "success" {
		t.Errorf("job-ok = %#v, want last run stamped with success", okJob)
	}
	failJob, ok := byID["job-fail"]
	if !ok || failJob.LastStatus != "failed" {
		t.Errorf("job-fail = %#v, want failed status", failJob)
	}
}

// TestListByWebsiteMigratesJobsWithoutWrappers upgrades pre-wrapper crontabs
// the first time the page is opened, so real runs start being tracked.
func TestListByWebsiteMigratesJobsWithoutWrappers(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")
	insertCronJob(t, db, "job-old", "web-001", "old command", "* * * * *")

	streamCalled := false
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// test -f wrapper: missing; every other call succeeds.
			if name == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			streamCalled = true
			return 0, nil
		},
	}
	svc := NewService(db, mock, nil)

	if _, err := svc.ListByWebsite(context.Background(), "web-001"); err != nil {
		t.Fatalf("ListByWebsite: %v", err)
	}
	if !streamCalled {
		t.Error("missing wrappers must trigger a crontab rebuild")
	}
}

func TestTestJobWithoutRunner(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")
	svc := NewService(db, mockExecutor(), nil)

	job, err := svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001",
		Command:   "echo ok",
		Schedule:  "* * * * *",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.TestJob(context.Background(), job.ID); err == nil {
		t.Fatal("expected an error without a task runner")
	}
}

func TestTestJobRunsCommandAndRecordsSuccess(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")

	var gotCmd []string
	exec := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			gotCmd = append(gotCmd, append([]string{name}, args...)...)
			return &executor.Result{ExitCode: 0, Stdout: "hello\n"}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			return 0, nil
		},
	}
	svc := NewService(db, exec, nil)
	tr := taskrunner.New()
	svc.SetTaskRunner(tr)

	job, err := svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001",
		Command:   "php artisan schedule:run",
		Schedule:  "* * * * *",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	taskID, err := svc.TestJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("TestJob: %v", err)
	}

	task := waitForTask(t, tr, taskID)
	if task.Status != "completed" {
		t.Fatalf("task status = %s (%s), want completed", task.Status, task.Error)
	}

	// The command must run as the website's user, from its home directory,
	// with a POSIX shell like cron uses.
	joined := strings.Join(gotCmd, " ")
	for _, want := range []string{"-H", "-u", "testuser", "sh", "-c", "cd ~ && php artisan schedule:run"} {
		if !strings.Contains(joined, want) {
			t.Errorf("command %q missing %q", joined, want)
		}
	}

	updated, err := svc.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.LastStatus != "success" {
		t.Errorf("last_status = %q, want success", updated.LastStatus)
	}
	if updated.LastRun.IsZero() {
		t.Error("last_run must be stamped after a test")
	}
}

func TestTestJobRecordsFailure(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "web-001", "testuser")

	exec := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// Only the actual job command fails; the wrapper/crontab
			// bookkeeping around it must succeed.
			if name == "sudo" {
				return &executor.Result{ExitCode: 1, Stderr: "command not found"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputStreamFunc: func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
			return 0, nil
		},
	}
	svc := NewService(db, exec, nil)
	tr := taskrunner.New()
	svc.SetTaskRunner(tr)

	job, err := svc.Create(context.Background(), CronJobRequest{
		WebsiteID: "web-001",
		Command:   "nope",
		Schedule:  "* * * * *",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	taskID, err := svc.TestJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("TestJob: %v", err)
	}

	task := waitForTask(t, tr, taskID)
	if task.Status != "failed" {
		t.Fatalf("task status = %s, want failed", task.Status)
	}

	updated, err := svc.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.LastStatus != "failed" {
		t.Errorf("last_status = %q, want failed", updated.LastStatus)
	}
}
