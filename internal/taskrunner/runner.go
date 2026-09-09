package taskrunner

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

type Task struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Module    string    `json:"module,omitempty"`
	Status    string    `json:"status"`
	Output    string    `json:"output"`
	Error     string    `json:"error"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Runner struct {
	mu          sync.RWMutex
	tasks       map[string]*Task
	store       Store
	lastPersist map[string]time.Time
}

func New() *Runner {
	return newRunner(nil, nil)
}

func NewPersistent(db *sql.DB) (*Runner, error) {
	store := NewSQLiteStore(db)
	now := time.Now().UTC()
	if err := store.FailRunning(context.Background(), now, "panel restarted before task completed"); err != nil {
		return nil, fmt.Errorf("mark interrupted tasks failed: %w", err)
	}
	tasks, err := store.LoadRecent(context.Background(), 200)
	if err != nil {
		return nil, fmt.Errorf("load background tasks: %w", err)
	}
	return newRunner(store, tasks), nil
}

func newRunner(store Store, restored []Task) *Runner {
	runner := &Runner{
		tasks:       make(map[string]*Task, len(restored)),
		store:       store,
		lastPersist: make(map[string]time.Time),
	}
	for i := range restored {
		task := restored[i]
		runner.tasks[task.ID] = &task
	}
	return runner
}

// Run executes a command in the background with live output streaming.
func (r *Runner) Run(name string, cmdName string, args ...string) string {
	task := r.startTask(Options{Name: name, Timeout: 30 * time.Minute})

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		cmdArgs := append([]string{cmdName}, args...)
		cmd := exec.CommandContext(ctx, "sudo", sudoArgs(cmdArgs)...)

		// Pipe stdout and stderr for live streaming
		stdoutPipe, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout // merge stderr into stdout

		if err := cmd.Start(); err != nil {
			r.finishTask(task.ID, fmt.Errorf("start: %w", err))
			return
		}

		// Read output line by line, update task in real-time
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			r.appendOutput(task.ID, scanner.Text()+"\n")
		}

		err := cmd.Wait()
		r.finishTask(task.ID, err)
	}()

	return task.ID
}

// RunMultiple runs multiple commands sequentially as one task with live output.
func (r *Runner) RunMultiple(name string, commands [][]string) string {
	task := r.startTask(Options{Name: name, Timeout: 30 * time.Minute})

	go func() {
		for i, cmdArgs := range commands {
			if len(cmdArgs) == 0 {
				continue
			}

			stepMsg := fmt.Sprintf("\n=== Step %d/%d: %s ===\n", i+1, len(commands), cmdArgs[0])
			r.appendOutput(task.ID, stepMsg)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			cmd := exec.CommandContext(ctx, "sudo", sudoArgs(cmdArgs)...)

			// Live output streaming
			pr, pw := io.Pipe()
			cmd.Stdout = pw
			cmd.Stderr = pw

			if err := cmd.Start(); err != nil {
				cancel()
				pw.Close()
				r.appendOutput(task.ID, fmt.Sprintf("ERROR: %v\n", err))
				r.finishTask(task.ID, fmt.Errorf("step %d start: %w", i+1, err))
				return
			}

			// Read output in goroutine
			done := make(chan struct{})
			go func() {
				scanner := bufio.NewScanner(pr)
				scanner.Buffer(make([]byte, 64*1024), 1024*1024)
				for scanner.Scan() {
					line := scanner.Text() + "\n"
					r.appendOutput(task.ID, line)
				}
				close(done)
			}()

			err := cmd.Wait()
			pw.Close()
			<-done
			cancel()

			if err != nil {
				r.appendOutput(task.ID, fmt.Sprintf("ERROR: %v\n", err))
				r.finishTask(task.ID, fmt.Errorf("step %d failed: %w", i+1, err))
				return
			}
		}
		r.finishTask(task.ID, nil)
	}()

	return task.ID
}

func sudoArgs(command []string) []string {
	args := make([]string, 0, len(command)+2)
	args = append(args, "env", "DEBIAN_FRONTEND=noninteractive")
	return append(args, command...)
}

func (r *Runner) Get(id string) (*Task, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	if !ok {
		return nil, false
	}
	copy := *t
	return &copy, true
}

func (r *Runner) List() []Task {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var tasks []Task
	for _, t := range r.tasks {
		tasks = append(tasks, *t)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt) })
	return tasks
}

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

const (
	maxTaskOutputBytes    = 512 * 1024
	outputTruncatedMarker = "[older output truncated]\n"
	persistInterval       = 250 * time.Millisecond
)

type Options struct {
	Name    string
	Module  string
	Timeout time.Duration
}

func (r *Runner) startTask(options Options) *Task {
	now := time.Now().UTC()
	task := &Task{
		ID:        ulid.Make().String(),
		Name:      options.Name,
		Module:    options.Module,
		Status:    "running",
		StartedAt: now,
		UpdatedAt: now,
	}
	r.mu.Lock()
	r.tasks[task.ID] = task
	copy := *task
	r.lastPersist[task.ID] = now
	r.mu.Unlock()
	r.persist(copy)
	return task
}

func (r *Runner) appendOutput(id, output string) {
	if output == "" {
		return
	}
	r.mu.Lock()
	task, ok := r.tasks[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	task.Output = boundOutput(task.Output + output)
	task.UpdatedAt = time.Now().UTC()
	shouldPersist := task.UpdatedAt.Sub(r.lastPersist[id]) >= persistInterval
	if shouldPersist {
		r.lastPersist[id] = task.UpdatedAt
	}
	copy := *task
	r.mu.Unlock()
	if shouldPersist {
		r.persist(copy)
	}
}

func (r *Runner) finishTask(id string, taskErr error) {
	r.mu.Lock()
	task, ok := r.tasks[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	task.EndedAt = now
	task.UpdatedAt = now
	if taskErr != nil {
		task.Status = "failed"
		task.Error = taskErr.Error()
	} else {
		task.Status = "completed"
	}
	copy := *task
	delete(r.lastPersist, id)
	r.mu.Unlock()
	r.persist(copy)
}

func (r *Runner) persist(task Task) {
	if r.store != nil {
		_ = r.store.Upsert(context.Background(), task)
	}
}

func boundOutput(output string) string {
	if len(output) <= maxTaskOutputBytes {
		return output
	}
	keep := maxTaskOutputBytes - len(outputTruncatedMarker)
	start := len(output) - keep
	for start < len(output) && output[start]&0xc0 == 0x80 {
		start++
	}
	return outputTruncatedMarker + output[start:]
}
