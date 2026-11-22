package update

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/dbmanager"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// capturingApt stands in for the sudo stream, recording the invoked command
// and exiting successfully without touching a real apt.
func capturingApt(t *testing.T) (*Service, *taskrunner.Runner, func() []string) {
	t.Helper()
	runner := taskrunner.New()
	svc := NewService(nil, "0.1.0", runner)
	var mu sync.Mutex
	var invoked []string
	svc.runSudoStream = func(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
		mu.Lock()
		invoked = append(append(invoked, name), args...)
		mu.Unlock()
		return 0, nil
	}
	return svc, runner, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return invoked
	}
}

func waitForTask(t *testing.T, runner *taskrunner.Runner, taskID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		task, ok := runner.Get(taskID)
		if ok && task.Status != "running" {
			if task.Status != "completed" {
				t.Fatalf("task status = %q, error = %q", task.Status, task.Error)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for apt task")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAptUpdateRunsNonInteractive(t *testing.T) {
	svc, runner, invoked := capturingApt(t)

	taskID, err := svc.AptUpdate()
	if err != nil {
		t.Fatalf("AptUpdate() error = %v", err)
	}
	waitForTask(t, runner, taskID)

	joined := strings.Join(invoked(), " ")
	for _, want := range []string{"env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "update"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("apt update command missing %q: %s", want, joined)
		}
	}
}

func TestAptUpgradeKeepsLocalConfigsAndAvoidsPrompt(t *testing.T) {
	svc, runner, invoked := capturingApt(t)

	taskID, err := svc.AptUpgrade()
	if err != nil {
		t.Fatalf("AptUpgrade() error = %v", err)
	}
	waitForTask(t, runner, taskID)

	joined := strings.Join(invoked(), " ")
	for _, want := range []string{
		"DEBIAN_FRONTEND=noninteractive",
		"apt-get",
		"upgrade",
		"-y",
		"--with-new-pkgs",
		"Dpkg::Lock::Timeout=120",
		"Dpkg::Options::=--force-confold",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("apt upgrade command missing %q: %s", want, joined)
		}
	}
}

// Two concurrent apt runs would fight over the dpkg lock: while one apt task
// is still running, starting another must be rejected.
func TestAptOperationsAreSerialized(t *testing.T) {
	blocked := make(chan struct{})
	release := make(chan struct{})
	runner := taskrunner.New()
	svc := NewService(nil, "0.1.0", runner)
	svc.runSudoStream = func(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
		if name != "env" {
			return 0, nil // the legacy PPA migration step
		}
		close(blocked)
		<-release
		return 0, nil
	}

	taskID, err := svc.AptUpdate()
	if err != nil {
		t.Fatalf("AptUpdate() error = %v", err)
	}
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("apt task never started")
	}

	if _, err := svc.AptUpgrade(); err == nil {
		t.Fatal("expected rejection while another apt operation is running")
	}
	close(release)
	waitForTask(t, runner, taskID)
}

// apt reports why it failed on stderr ("E: ..."); the task log must show it,
// not just the exit code.
func TestAptUpdateLogsStderr(t *testing.T) {
	runner := taskrunner.New()
	svc := NewService(&executor.MockExecutor{
		RunSudoStreamSplitFunc: func(ctx context.Context, stdoutW, stderrW io.Writer, name string, args ...string) (int, error) {
			if name != "env" {
				return 0, nil
			}
			io.WriteString(stdoutW, "Reading package lists...\n")
			io.WriteString(stderrW, "E: The repository is not signed.\n")
			return 100, nil
		},
	}, "0.1.0", runner)

	taskID, err := svc.AptUpdate()
	if err != nil {
		t.Fatalf("AptUpdate() error = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		task, _ := runner.Get(taskID)
		if task != nil && task.Status != "running" {
			if task.Status != "failed" {
				t.Fatalf("status = %q, want failed", task.Status)
			}
			if !strings.Contains(task.Output, "E: The repository is not signed.") {
				t.Fatalf("stderr missing from task output:\n%s", task.Output)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for apt task")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A server still carrying the deprecated ondrej PPA cannot run apt-get
// update; the PPA is migrated to packages.sury.org before apt runs.
func TestAptUpdateMigratesLegacyPHPPPAFirst(t *testing.T) {
	svc, runner, invoked := capturingApt(t)
	taskID, err := svc.AptUpdate()
	if err != nil {
		t.Fatal(err)
	}
	waitForTask(t, runner, taskID)
	joined := strings.Join(invoked(), " ")
	migrate := strings.Index(joined, "packages.sury.org/php")
	apt := strings.Index(joined, "apt-get update")
	if migrate < 0 || apt < 0 || migrate > apt {
		t.Fatalf("legacy PPA migration must run before apt-get update: %s", joined)
	}
}

// The pgdg meta package pin is written before every apt run, so servers
// that added pgdg earlier (pgvector) stop pulling the newest major.
func TestAptUpgradePinsPGDGMetaPackagesFirst(t *testing.T) {
	svc, runner, invoked := capturingApt(t)
	taskID, err := svc.AptUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	waitForTask(t, runner, taskID)
	joined := strings.Join(invoked(), " ")
	pin := strings.Index(joined, dbmanager.PGDGPinPath)
	apt := strings.Index(joined, "apt-get upgrade")
	if pin < 0 || apt < 0 || pin > apt {
		t.Fatalf("the pgdg pin must be written before apt-get upgrade: %s", joined)
	}
}
