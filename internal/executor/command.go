package executor

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// Result holds the output and metadata from a command execution.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// CommandExecutor defines the interface for running system commands.
type CommandExecutor interface {
	Run(ctx context.Context, name string, args ...string) (*Result, error)
	RunSudo(ctx context.Context, name string, args ...string) (*Result, error)
}

// Executor implements CommandExecutor with a configurable default timeout.
type Executor struct {
	DefaultTimeout time.Duration
}

// NewExecutor creates a new Executor with the given default timeout.
func NewExecutor(defaultTimeout time.Duration) *Executor {
	return &Executor{DefaultTimeout: defaultTimeout}
}

// Run executes a command. If the context has no deadline, the default timeout
// is applied. A non-zero exit code is not treated as an error; the exit code
// is captured in Result.ExitCode.
func (e *Executor) Run(ctx context.Context, name string, args ...string) (*Result, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.DefaultTimeout)
		defer cancel()
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := &Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
		Duration: duration,
	}

	if err != nil {
		// If the context expired (timeout or cancel), treat as an error
		// even if the process exited with a signal.
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, err
	}

	return result, nil
}

// RunSudo executes a command with sudo by prepending /usr/bin/sudo.
func (e *Executor) RunSudo(ctx context.Context, name string, args ...string) (*Result, error) {
	sudoArgs := make([]string, 0, len(args)+1)
	sudoArgs = append(sudoArgs, name)
	sudoArgs = append(sudoArgs, args...)
	return e.Run(ctx, "/usr/bin/sudo", sudoArgs...)
}

// MockExecutor is a test double for CommandExecutor.
type MockExecutor struct {
	RunFunc     func(ctx context.Context, name string, args ...string) (*Result, error)
	RunSudoFunc func(ctx context.Context, name string, args ...string) (*Result, error)
}

// Run delegates to RunFunc.
func (m *MockExecutor) Run(ctx context.Context, name string, args ...string) (*Result, error) {
	return m.RunFunc(ctx, name, args...)
}

// RunSudo delegates to RunSudoFunc.
func (m *MockExecutor) RunSudo(ctx context.Context, name string, args ...string) (*Result, error) {
	return m.RunSudoFunc(ctx, name, args...)
}
