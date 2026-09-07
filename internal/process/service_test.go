package process

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const samplePSOutput = `USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND
root         1  0.0  0.1 169348 11636 ?        Ss   Jun01   0:03 /sbin/init
www-data  1234 25.3  4.2 512000 43008 ?        Sl   10:30   1:15 nginx: worker process
postgres  5678  5.1  8.7 256000 89088 ?        Ss   Jun01   5:42 /usr/lib/postgresql/14/bin/postgres -D /var/lib/postgresql/14/main`

func TestParseProcesses(t *testing.T) {
	procs := parsePS(samplePSOutput)
	if len(procs) != 3 {
		t.Fatalf("expected 3 processes, got %d", len(procs))
	}

	// First process: init
	p := procs[0]
	if p.PID != 1 {
		t.Errorf("expected PID 1, got %d", p.PID)
	}
	if p.User != "root" {
		t.Errorf("expected user root, got %q", p.User)
	}
	if p.CPU != 0.0 {
		t.Errorf("expected CPU 0.0, got %f", p.CPU)
	}
	if p.Command != "/sbin/init" {
		t.Errorf("expected command /sbin/init, got %q", p.Command)
	}

	// Second process: nginx worker
	p = procs[1]
	if p.PID != 1234 {
		t.Errorf("expected PID 1234, got %d", p.PID)
	}
	if p.User != "www-data" {
		t.Errorf("expected user www-data, got %q", p.User)
	}
	if p.CPU != 25.3 {
		t.Errorf("expected CPU 25.3, got %f", p.CPU)
	}
	if p.RAM != 4.2 {
		t.Errorf("expected RAM 4.2, got %f", p.RAM)
	}
	if p.VSZ != 512000 {
		t.Errorf("expected VSZ 512000, got %d", p.VSZ)
	}
	if p.RSS != 43008 {
		t.Errorf("expected RSS 43008, got %d", p.RSS)
	}
	if p.Command != "nginx: worker process" {
		t.Errorf("expected command 'nginx: worker process', got %q", p.Command)
	}

	// Third process: postgres
	p = procs[2]
	if p.PID != 5678 {
		t.Errorf("expected PID 5678, got %d", p.PID)
	}
	if !strings.Contains(p.Command, "postgres") {
		t.Errorf("expected command to contain 'postgres', got %q", p.Command)
	}
}

func TestKill_RejectPID1(t *testing.T) {
	mock := &executor.MockExecutor{}
	svc := NewService(mock)

	err := svc.Kill(context.Background(), 1, "TERM")
	if err == nil {
		t.Fatal("expected error when killing PID 1")
	}

	domainErr, ok := err.(*model.DomainError)
	if !ok {
		t.Fatalf("expected DomainError, got %T", err)
	}
	if domainErr.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %q", domainErr.Code)
	}
}

func TestKill_RejectBadSignal(t *testing.T) {
	mock := &executor.MockExecutor{}
	svc := NewService(mock)

	err := svc.Kill(context.Background(), 9999, "DESTROY")
	if err == nil {
		t.Fatal("expected error for invalid signal DESTROY")
	}

	domainErr, ok := err.(*model.DomainError)
	if !ok {
		t.Fatalf("expected DomainError, got %T", err)
	}
	if domainErr.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %q", domainErr.Code)
	}
	if !strings.Contains(domainErr.Message, "DESTROY") {
		t.Errorf("expected message to mention DESTROY, got %q", domainErr.Message)
	}
}

func TestKill_ValidSignal(t *testing.T) {
	var capturedName string
	var capturedArgs []string

	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			capturedName = name
			capturedArgs = args
			return &executor.Result{Stdout: "", ExitCode: 0}, nil
		},
	}

	svc := NewService(mock)

	err := svc.Kill(context.Background(), 4242, "KILL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedName != "kill" {
		t.Errorf("expected command 'kill', got %q", capturedName)
	}
	if len(capturedArgs) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(capturedArgs), capturedArgs)
	}
	if capturedArgs[0] != "-KILL" {
		t.Errorf("expected first arg '-KILL', got %q", capturedArgs[0])
	}
	if capturedArgs[1] != "4242" {
		t.Errorf("expected second arg '4242', got %q", capturedArgs[1])
	}
}
