package taskrunner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Task represents a background installation/operation task.
type Task struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // running, completed, failed
	Output    string    `json:"output"`
	Error     string    `json:"error"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

// Runner manages background tasks with output capture.
type Runner struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// New creates a new task runner.
func New() *Runner {
	return &Runner{tasks: make(map[string]*Task)}
}

// Run starts a background task. Returns task ID immediately.
// The command runs with sudo and captures all output.
func (r *Runner) Run(name string, cmdName string, args ...string) string {
	id := ulid.Make().String()
	task := &Task{
		ID:        id,
		Name:      name,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}

	r.mu.Lock()
	r.tasks[id] = task
	r.mu.Unlock()

	go func() {
		var stdout, stderr bytes.Buffer

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sudo", append([]string{cmdName}, args...)...)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")

		err := cmd.Run()

		r.mu.Lock()
		defer r.mu.Unlock()

		task.Output = stdout.String() + stderr.String()
		task.EndedAt = time.Now().UTC()

		if err != nil {
			task.Status = "failed"
			task.Error = err.Error()
		} else {
			task.Status = "completed"
		}
	}()

	return id
}

// RunMultiple runs multiple commands sequentially as one task.
func (r *Runner) RunMultiple(name string, commands [][]string) string {
	id := ulid.Make().String()
	task := &Task{
		ID:        id,
		Name:      name,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}

	r.mu.Lock()
	r.tasks[id] = task
	r.mu.Unlock()

	go func() {
		var allOutput string

		for i, cmdArgs := range commands {
			if len(cmdArgs) == 0 {
				continue
			}

			stepMsg := fmt.Sprintf("\n=== Step %d/%d: %s ===\n", i+1, len(commands), cmdArgs[0])

			r.mu.Lock()
			task.Output += stepMsg
			r.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)

			var stdout, stderr bytes.Buffer
			cmd := exec.CommandContext(ctx, "sudo", cmdArgs...)
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			cmd.Env = append(cmd.Environ(), "DEBIAN_FRONTEND=noninteractive")

			err := cmd.Run()
			cancel()

			output := stdout.String() + stderr.String()
			allOutput += stepMsg + output

			r.mu.Lock()
			task.Output += output
			r.mu.Unlock()

			if err != nil {
				r.mu.Lock()
				task.Status = "failed"
				task.Error = fmt.Sprintf("step %d failed: %v", i+1, err)
				task.EndedAt = time.Now().UTC()
				r.mu.Unlock()
				return
			}
		}

		r.mu.Lock()
		task.Status = "completed"
		task.Output = allOutput
		task.EndedAt = time.Now().UTC()
		r.mu.Unlock()
	}()

	return id
}

// Get returns a task by ID.
func (r *Runner) Get(id string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	if !ok {
		return nil, false
	}
	// Return a copy
	copy := *t
	return &copy, true
}

// List returns all tasks, newest first.
func (r *Runner) List() []Task {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tasks []Task
	for _, t := range r.tasks {
		tasks = append(tasks, *t)
	}
	return tasks
}

// Cleanup removes completed tasks older than duration.
func (r *Runner) Cleanup(maxAge time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, t := range r.tasks {
		if t.Status != "running" && t.EndedAt.Before(cutoff) {
			delete(r.tasks, id)
		}
	}
}

// HasRunning returns true if any task is currently running.
func (r *Runner) HasRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tasks {
		if t.Status == "running" {
			return true
		}
	}
	return false
}
