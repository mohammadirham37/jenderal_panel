package taskrunner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

type Task struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Output    string    `json:"output"`
	Error     string    `json:"error"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

type Runner struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

func New() *Runner {
	return &Runner{tasks: make(map[string]*Task)}
}

// Run executes a command in the background with live output streaming.
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		cmdArgs := append([]string{cmdName}, args...)
		cmd := exec.CommandContext(ctx, "sudo", sudoArgs(cmdArgs)...)

		// Pipe stdout and stderr for live streaming
		stdoutPipe, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout // merge stderr into stdout

		if err := cmd.Start(); err != nil {
			r.mu.Lock()
			task.Status = "failed"
			task.Error = fmt.Sprintf("start: %v", err)
			task.EndedAt = time.Now().UTC()
			r.mu.Unlock()
			return
		}

		// Read output line by line, update task in real-time
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			r.mu.Lock()
			task.Output += line
			r.mu.Unlock()
		}

		err := cmd.Wait()

		r.mu.Lock()
		task.EndedAt = time.Now().UTC()
		if err != nil {
			task.Status = "failed"
			task.Error = err.Error()
		} else {
			task.Status = "completed"
		}
		r.mu.Unlock()
	}()

	return id
}

// RunMultiple runs multiple commands sequentially as one task with live output.
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
		for i, cmdArgs := range commands {
			if len(cmdArgs) == 0 {
				continue
			}

			stepMsg := fmt.Sprintf("\n=== Step %d/%d: %s ===\n", i+1, len(commands), cmdArgs[0])
			r.mu.Lock()
			task.Output += stepMsg
			r.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			cmd := exec.CommandContext(ctx, "sudo", sudoArgs(cmdArgs)...)

			// Live output streaming
			pr, pw := io.Pipe()
			cmd.Stdout = pw
			cmd.Stderr = pw

			if err := cmd.Start(); err != nil {
				cancel()
				pw.Close()
				r.mu.Lock()
				task.Output += fmt.Sprintf("ERROR: %v\n", err)
				task.Status = "failed"
				task.Error = fmt.Sprintf("step %d start: %v", i+1, err)
				task.EndedAt = time.Now().UTC()
				r.mu.Unlock()
				return
			}

			// Read output in goroutine
			done := make(chan struct{})
			go func() {
				scanner := bufio.NewScanner(pr)
				scanner.Buffer(make([]byte, 64*1024), 1024*1024)
				for scanner.Scan() {
					line := scanner.Text() + "\n"
					r.mu.Lock()
					task.Output += line
					r.mu.Unlock()
				}
				close(done)
			}()

			err := cmd.Wait()
			pw.Close()
			<-done
			cancel()

			if err != nil {
				r.mu.Lock()
				task.Output += fmt.Sprintf("ERROR: %v\n", err)
				task.Status = "failed"
				task.Error = fmt.Sprintf("step %d failed: %v", i+1, err)
				task.EndedAt = time.Now().UTC()
				r.mu.Unlock()
				return
			}
		}

		r.mu.Lock()
		task.Status = "completed"
		task.EndedAt = time.Now().UTC()
		r.mu.Unlock()
	}()

	return id
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
