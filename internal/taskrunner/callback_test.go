package taskrunner

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCallbackTasksRetainProgressAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			r := New()
			started := make(chan struct{})
			release := make(chan struct{})
			id := r.RunFunc("test", func(ctx context.Context, log func(string)) error {
				log("first step")
				close(started)
				<-release
				if fail {
					return fmt.Errorf("migration failed")
				}
				return nil
			})
			<-started
			task, ok := r.Get(id)
			if !ok || task.Status != "running" || !strings.Contains(task.Output, "first step") {
				t.Fatal("progress not available while running")
			}
			close(release)
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				task, _ = r.Get(id)
				if task.Status != "running" {
					break
				}
				time.Sleep(time.Millisecond)
			}
			expected := "completed"
			if fail {
				expected = "failed"
			}
			if task.Status != expected {
				t.Fatalf("status=%s", task.Status)
			}
			if fail && task.Error != "migration failed" {
				t.Fatal("failure missing")
			}
		})
	}
}
