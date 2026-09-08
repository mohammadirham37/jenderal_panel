package taskrunner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunMultiplePreservesNonInteractiveEnvironmentThroughSudo(t *testing.T) {
	binDir := t.TempDir()
	sudoPath := filepath.Join(binDir, "sudo")
	script := `#!/bin/sh
if [ "$1" = "env" ] && [ "$2" = "DEBIAN_FRONTEND=noninteractive" ]; then
    exec "$@"
fi
unset DEBIAN_FRONTEND
exec "$@"
`
	if err := os.WriteFile(sudoPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := New()
	id := runner.RunMultiple("noninteractive install", [][]string{
		{"sh", "-c", `test "$DEBIAN_FRONTEND" = noninteractive`},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, ok := runner.Get(id)
		if !ok {
			t.Fatal("task disappeared")
		}
		if task.Status != "running" {
			if task.Status != "completed" {
				t.Fatalf("expected completed task, got %s: %s", task.Status, task.Error)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("task did not finish")
}
