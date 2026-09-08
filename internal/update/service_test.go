package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

func TestNewService(t *testing.T) {
	svc := NewService(nil, "0.1.0", nil)
	if svc.currentVer != "0.1.0" {
		t.Errorf("currentVer = %q, want 0.1.0", svc.currentVer)
	}
}

func TestUpdateAtomicallyReplacesRunningBinary(t *testing.T) {
	tempDir := t.TempDir()
	captureFile := filepath.Join(tempDir, "update-script")
	fakeSudo := filepath.Join(tempDir, "sudo")

	if err := os.WriteFile(fakeSudo, []byte("#!/bin/sh\nprintf '%s' \"$5\" > \"$CAPTURE_FILE\"\n"), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	t.Setenv("CAPTURE_FILE", captureFile)
	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := taskrunner.New()
	svc := NewService(nil, "0.1.0", runner)
	taskID, err := svc.Update(context.Background())
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		task, ok := runner.Get(taskID)
		if ok && task.Status != "running" {
			if task.Status != "completed" {
				t.Fatalf("update task status = %q, error = %q", task.Status, task.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for update task")
		}
		time.Sleep(10 * time.Millisecond)
	}

	scriptBytes, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("read captured update script: %v", err)
	}
	script := string(scriptBytes)
	if strings.Contains(script, "%!") {
		t.Fatalf("update script contains fmt formatting errors:\n%s", script)
	}
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}

	if strings.Contains(script, "cp /tmp/jenderal-update "+execPath) {
		t.Fatal("update script copies over the running executable and can fail with ETXTBSY")
	}
	if !strings.Contains(script, execPath+".new") {
		t.Fatalf("update script does not stage replacement beside executable:\n%s", script)
	}
	buildCommand := "go build -o " + execPath + ".new"
	backupCommand := "cp -f " + execPath + " " + execPath + ".bak"
	renameCommand := "mv -f " + execPath + ".new " + execPath
	buildIndex := strings.Index(script, buildCommand)
	backupIndex := strings.Index(script, backupCommand)
	renameIndex := strings.Index(script, renameCommand)
	if buildIndex < 0 || backupIndex < buildIndex || renameIndex < backupIndex {
		t.Fatalf("update script must build, back up, then atomically rename the exact binary paths:\n%s", script)
	}
	if !strings.Contains(script, "systemd-run --quiet --collect") || !strings.Contains(script, "--on-active=5s") {
		t.Fatalf("update script does not schedule restart outside the panel service cgroup:\n%s", script)
	}
	if !strings.Contains(script, "systemctl is-active --quiet jenderal") || !strings.Contains(script, execPath+".bak") {
		t.Fatalf("update script does not verify the restarted service and retain a rollback path:\n%s", script)
	}
	scheduleIndex := strings.Index(script, "if ! systemd-run")
	if scheduleIndex < 0 {
		t.Fatalf("update script has no guarded restart scheduling command:\n%s", script)
	}
	failureCopyIndex := strings.Index(script[scheduleIndex:], "cp -f "+execPath+".bak "+execPath+".rollback")
	failureRenameIndex := strings.Index(script[scheduleIndex:], "mv -f "+execPath+".rollback "+execPath)
	if failureCopyIndex < 0 || failureRenameIndex < failureCopyIndex {
		t.Fatalf("update script does not restore the verified backup when restart scheduling fails:\n%s", script)
	}
}

func TestUpdateRejectsConcurrentSelfUpdate(t *testing.T) {
	tempDir := t.TempDir()
	releaseFile := filepath.Join(tempDir, "release")
	fakeSudo := filepath.Join(tempDir, "sudo")
	if err := os.WriteFile(fakeSudo, []byte("#!/bin/sh\nwhile [ ! -f \"$RELEASE_FILE\" ]; do sleep 0.01; done\n"), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	t.Setenv("RELEASE_FILE", releaseFile)
	t.Setenv("PATH", tempDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := taskrunner.New()
	svc := NewService(nil, "0.1.0", runner)
	firstTaskID, err := svc.Update(context.Background())
	if err != nil {
		t.Fatalf("first Update() error = %v", err)
	}

	if _, err := svc.Update(context.Background()); err == nil {
		_ = os.WriteFile(releaseFile, nil, 0o600)
		t.Fatal("second Update() succeeded while the first update was still running")
	}

	if err := os.WriteFile(releaseFile, nil, 0o600); err != nil {
		t.Fatalf("release update task: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		task, ok := runner.Get(firstTaskID)
		if ok && task.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for first update task")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
