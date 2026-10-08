package executor

import (
	"context"
	"io"
	"os/exec"
	"syscall"
)

// Session is a long-running process connected through pipes. The terminal
// uses it to keep one shell alive across commands so state such as the
// working directory and exported variables persists between commands.
type Session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	done   chan struct{}
}

// StartSession starts a long-running process with piped stdio. Unlike Run,
// no default timeout is applied; the caller's context governs the process
// lifetime and its cancellation terminates the process.
func (e *Executor) StartSession(ctx context.Context, name string, args ...string) (*Session, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	configureGracefulCancellation(cmd)
	configureProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	s := &Session{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(s.done)
	}()
	return s, nil
}

// Stdin returns the process standard input.
func (s *Session) Stdin() io.WriteCloser { return s.stdin }

// Stdout returns the process standard output.
func (s *Session) Stdout() io.ReadCloser { return s.stdout }

// Stderr returns the process standard error.
func (s *Session) Stderr() io.ReadCloser { return s.stderr }

// Done is closed when the process exits.
func (s *Session) Done() <-chan struct{} { return s.done }

// Stop kills the process group and waits for the process to exit.
func (s *Session) Stop() error {
	err := killProcessGroup(s.cmd)
	<-s.done
	return err
}

// Signal delivers sig to the session's process group, so a terminal-style
// interrupt reaches the running foreground command without ending the shell
// (which guards itself with a no-op trap).
func (s *Session) Signal(sig syscall.Signal) error {
	return signalProcessGroup(s.cmd, sig)
}
