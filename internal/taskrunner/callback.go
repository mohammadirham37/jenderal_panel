package taskrunner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// RunFunc keeps multi-step service work visible through the same task API as
// command tasks. Its lifetime is independent of the initiating HTTP request.
func (r *Runner) RunFunc(name string, work func(context.Context, func(string)) error) string {
	task := &Task{ID: ulid.Make().String(), Name: name, Status: "running", StartedAt: time.Now().UTC()}
	r.mu.Lock()
	r.tasks[task.ID] = task
	r.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var err error
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("background task panicked: %v", recovered)
			}
			r.mu.Lock()
			defer r.mu.Unlock()
			task.EndedAt = time.Now().UTC()
			if err != nil {
				task.Status = "failed"
				task.Error = err.Error()
			} else {
				task.Status = "completed"
			}
		}()
		log := func(output string) {
			if output == "" {
				return
			}
			if !strings.HasSuffix(output, "\n") {
				output += "\n"
			}
			r.mu.Lock()
			task.Output += output
			r.mu.Unlock()
		}
		err = work(ctx, log)
		if err == nil {
			err = ctx.Err()
		}
	}()
	return task.ID
}
