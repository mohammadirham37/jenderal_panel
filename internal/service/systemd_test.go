package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func newMockExecutor(stdout string, exitCode int, err error) *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{Stdout: stdout, ExitCode: exitCode, Duration: time.Millisecond}, err
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{Stdout: stdout, ExitCode: exitCode, Duration: time.Millisecond}, err
		},
	}
}

func TestParseStatusOutput(t *testing.T) {
	output := strings.Join([]string{
		"ActiveState=active",
		"SubState=running",
		"MainPID=1234",
		"UnitFileState=enabled",
	}, "\n")

	mock := newMockExecutor(output, 0, nil)
	mgr := NewSystemd(mock, []string{"nginx"})

	status, err := mgr.Status(context.Background(), "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Name != "nginx" {
		t.Errorf("expected name 'nginx', got %q", status.Name)
	}
	if !status.Active {
		t.Error("expected Active to be true")
	}
	if !status.Running {
		t.Error("expected Running to be true")
	}
	if !status.Enabled {
		t.Error("expected Enabled to be true")
	}
	if status.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", status.PID)
	}
}

func TestNotAllowedRejection(t *testing.T) {
	mock := newMockExecutor("", 0, nil)
	mgr := NewSystemd(mock, []string{"nginx", "mysql"})

	ctx := context.Background()

	_, err := mgr.Status(ctx, "redis")
	if !errors.Is(err, model.ErrServiceNotAllowed) {
		t.Fatalf("expected ErrServiceNotAllowed, got: %v", err)
	}

	err = mgr.Start(ctx, "redis")
	if !errors.Is(err, model.ErrServiceNotAllowed) {
		t.Fatalf("expected ErrServiceNotAllowed for Start, got: %v", err)
	}

	err = mgr.Stop(ctx, "redis")
	if !errors.Is(err, model.ErrServiceNotAllowed) {
		t.Fatalf("expected ErrServiceNotAllowed for Stop, got: %v", err)
	}

	err = mgr.Restart(ctx, "redis")
	if !errors.Is(err, model.ErrServiceNotAllowed) {
		t.Fatalf("expected ErrServiceNotAllowed for Restart, got: %v", err)
	}

	err = mgr.Reload(ctx, "redis")
	if !errors.Is(err, model.ErrServiceNotAllowed) {
		t.Fatalf("expected ErrServiceNotAllowed for Reload, got: %v", err)
	}
}

func TestRestartCapturesCorrectArgs(t *testing.T) {
	var capturedName string
	var capturedArgs []string

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			capturedName = name
			capturedArgs = args
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
	}

	mgr := NewSystemd(mock, []string{"nginx"})
	err := mgr.Restart(context.Background(), "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedName != "systemctl" {
		t.Errorf("expected command 'systemctl', got %q", capturedName)
	}
	expectedArgs := []string{"restart", "nginx"}
	if len(capturedArgs) != len(expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, capturedArgs)
	}
	for i, a := range expectedArgs {
		if capturedArgs[i] != a {
			t.Errorf("arg[%d]: expected %q, got %q", i, a, capturedArgs[i])
		}
	}
}

func TestGlobMatching(t *testing.T) {
	mock := newMockExecutor("ActiveState=active\nSubState=running\nMainPID=100\nUnitFileState=enabled\n", 0, nil)
	mgr := NewSystemd(mock, []string{"php*-fpm"})

	// php8.4-fpm should match the glob pattern php*-fpm.
	status, err := mgr.Status(context.Background(), "php8.4-fpm")
	if err != nil {
		t.Fatalf("expected php8.4-fpm to match glob php*-fpm, got error: %v", err)
	}
	if status.Name != "php8.4-fpm" {
		t.Errorf("expected name 'php8.4-fpm', got %q", status.Name)
	}

	// redis should not match.
	_, err = mgr.Status(context.Background(), "redis")
	if !errors.Is(err, model.ErrServiceNotAllowed) {
		t.Fatalf("expected ErrServiceNotAllowed for 'redis', got: %v", err)
	}
}

func TestFailedCommandError(t *testing.T) {
	mock := newMockExecutor("", 1, nil)
	mock.RunSudoFunc = func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{
			Stderr:   "Failed to start nginx.service: Unit not found.",
			ExitCode: 5,
			Duration: time.Millisecond,
		}, nil
	}
	mgr := NewSystemd(mock, []string{"nginx"})

	err := mgr.Start(context.Background(), "nginx")
	if err == nil {
		t.Fatal("expected error for non-zero exit code, got nil")
	}
	if !strings.Contains(err.Error(), "Unit not found") {
		t.Errorf("expected error to contain 'Unit not found', got: %v", err)
	}
}
