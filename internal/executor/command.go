package executor

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	RunSudoWithInput(ctx context.Context, input, name string, args ...string) (*Result, error)
	RunSudoStream(ctx context.Context, w io.Writer, name string, args ...string) (int, error)
	RunSudoWithInputStream(ctx context.Context, stdin io.Reader, stderr io.Writer, name string, args ...string) (int, error)
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
	return e.run(ctx, nil, name, args...)
}

func (e *Executor) run(ctx context.Context, input *string, name string, args ...string) (*Result, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.DefaultTimeout)
		defer cancel()
	}

	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	configureGracefulCancellation(cmd)
	if input != nil {
		cmd.Stdin = bytes.NewBufferString(*input)
	}
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

// RunSudoWithInput executes a sudo command and provides literal standard input
// without involving a shell.
func (e *Executor) RunSudoWithInput(ctx context.Context, input, name string, args ...string) (*Result, error) {
	sudoArgs := make([]string, 0, len(args)+1)
	sudoArgs = append(sudoArgs, name)
	sudoArgs = append(sudoArgs, args...)
	return e.run(ctx, &input, "/usr/bin/sudo", sudoArgs...)
}

// RunStream executes the command and streams merged stdout and stderr to w
// without buffering the whole output in memory. It returns the exit code.
func (e *Executor) RunStream(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
	return e.stream(ctx, w, nil, nil, name, args...)
}

// RunSudoStream executes the command as root and streams merged stdout and
// stderr to w without buffering the whole output in memory. It returns the
// process exit code.
func (e *Executor) RunSudoStream(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
	sudoArgs := append([]string{name}, args...)
	return e.stream(ctx, w, nil, nil, "/usr/bin/sudo", sudoArgs...)
}

// RunSudoWithInputStream executes the command as root with stdin streamed
// from reader, avoiding buffering the whole input in memory; stderr streams
// to stderrW for error reporting. It returns the process exit code.
func (e *Executor) RunSudoWithInputStream(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
	sudoArgs := append([]string{name}, args...)
	return e.stream(ctx, nil, stderrW, stdin, "/usr/bin/sudo", sudoArgs...)
}

// stream executes a command whose stdout streams into out (nil discards),
// stderr into stderrW (nil discards), and stdin from in (nil for no input).
//
// It deliberately skips the graceful-cancellation setup used by Run: a
// non-nil Cancel makes os/exec close the child's stdin as soon as Wait is
// entered, which would starve stdin-streaming commands of their input. The
// command is still killed when ctx is canceled (CommandContext default).
func (e *Executor) stream(ctx context.Context, out io.Writer, stderrW io.Writer, in io.Reader, name string, args ...string) (int, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.DefaultTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if out != nil {
		cmd.Stdout = out
	}
	if stderrW != nil {
		cmd.Stderr = stderrW
	}
	if in != nil {
		cmd.Stdin = in
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	waitErr := cmd.Wait()

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return -1, waitErr
		}
	}
	return exitCode, nil
}

// MockExecutor is a test double for CommandExecutor.
type MockExecutor struct {
	RunFunc                    func(ctx context.Context, name string, args ...string) (*Result, error)
	RunSudoFunc                func(ctx context.Context, name string, args ...string) (*Result, error)
	RunSudoWithInputFunc       func(ctx context.Context, input, name string, args ...string) (*Result, error)
	RunSudoStreamFunc          func(ctx context.Context, w io.Writer, name string, args ...string) (int, error)
	RunSudoWithInputStreamFunc func(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error)
}

// Run delegates to RunFunc.
func (m *MockExecutor) Run(ctx context.Context, name string, args ...string) (*Result, error) {
	return m.RunFunc(ctx, name, args...)
}

// RunSudo delegates to RunSudoFunc.
func (m *MockExecutor) RunSudo(ctx context.Context, name string, args ...string) (*Result, error) {
	return m.RunSudoFunc(ctx, name, args...)
}

// RunSudoWithInput delegates to RunSudoWithInputFunc.
func (m *MockExecutor) RunSudoWithInput(ctx context.Context, input, name string, args ...string) (*Result, error) {
	return m.RunSudoWithInputFunc(ctx, input, name, args...)
}

// RunSudoWithInputStream delegates to RunSudoWithInputStreamFunc.
func (m *MockExecutor) RunSudoWithInputStream(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
	if m.RunSudoWithInputStreamFunc != nil {
		return m.RunSudoWithInputStreamFunc(ctx, stdin, stderrW, name, args...)
	}
	return 0, nil
}

// RunSudoStream delegates to RunSudoStreamFunc.
func (m *MockExecutor) RunSudoStream(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
	if m.RunSudoStreamFunc != nil {
		return m.RunSudoStreamFunc(ctx, w, name, args...)
	}
	return 0, nil
}
