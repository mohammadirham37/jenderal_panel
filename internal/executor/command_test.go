package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunEcho(t *testing.T) {
	e := NewExecutor(30 * time.Second)
	ctx := context.Background()

	result, err := e.Run(ctx, "echo", "hello", "world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	got := strings.TrimSpace(result.Stdout)
	if got != "hello world" {
		t.Fatalf("expected stdout 'hello world', got %q", got)
	}
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}

func TestRunTimeout(t *testing.T) {
	e := NewExecutor(100 * time.Millisecond)
	ctx := context.Background() // no deadline, so default 100ms timeout applies

	_, err := e.Run(ctx, "sleep", "10")
	if err == nil {
		t.Fatal("expected error due to timeout, got nil")
	}
}

func TestRunContextCancel(t *testing.T) {
	e := NewExecutor(30 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := e.Run(ctx, "sleep", "10")
	if err == nil {
		t.Fatal("expected error due to cancelled context, got nil")
	}
}

func TestRunContextCancelAllowsCleanupTrap(t *testing.T) {
	cleanupPath := filepath.Join(t.TempDir(), "cleanup")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := NewExecutor(time.Minute).Run(ctx, "/bin/bash", "-c", `trap 'printf cleaned > "$1"; exit 143' TERM; while :; do sleep .02; done`, "--", cleanupPath)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	if got, readErr := os.ReadFile(cleanupPath); readErr != nil || string(got) != "cleaned" {
		t.Fatalf("cleanup = %q, %v", got, readErr)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	e := NewExecutor(30 * time.Second)
	ctx := context.Background()

	result, err := e.Run(ctx, "sh", "-c", "exit 42")
	if err != nil {
		t.Fatalf("non-zero exit should not return error, got: %v", err)
	}
	if result.ExitCode != 42 {
		t.Fatalf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestRunWithInputPassesLiteralStdin(t *testing.T) {
	e := NewExecutor(30 * time.Second)
	input := "$(not-a-command) ' literal"
	result, err := e.run(context.Background(), &input, "cat")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if result.Stdout != input {
		t.Fatalf("stdout = %q, want literal input %q", result.Stdout, input)
	}
}

func TestMockExecutor(t *testing.T) {
	mock := &MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*Result, error) {
			return &Result{
				Stdout:   "mocked output",
				ExitCode: 0,
				Duration: 5 * time.Millisecond,
			}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*Result, error) {
			return &Result{
				Stdout:   "mocked sudo output",
				ExitCode: 0,
				Duration: 5 * time.Millisecond,
			}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*Result, error) {
			return &Result{Stdout: input, ExitCode: 0}, nil
		},
	}

	// Verify it satisfies the interface.
	var _ CommandExecutor = mock

	ctx := context.Background()

	result, err := mock.Run(ctx, "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "mocked output" {
		t.Fatalf("expected 'mocked output', got %q", result.Stdout)
	}

	result, err = mock.RunSudo(ctx, "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "mocked sudo output" {
		t.Fatalf("expected 'mocked sudo output', got %q", result.Stdout)
	}

	result, err = mock.RunSudoWithInput(ctx, "literal input", "anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stdout != "literal input" {
		t.Fatalf("expected literal input, got %q", result.Stdout)
	}
}
