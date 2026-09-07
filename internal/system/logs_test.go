package system

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestValidateLogPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		allowed bool
	}{
		{
			name:    "nginx access log",
			path:    "/var/log/nginx/access.log",
			allowed: true,
		},
		{
			name:    "nginx error log",
			path:    "/var/log/nginx/error.log",
			allowed: true,
		},
		{
			name:    "syslog exact",
			path:    "/var/log/syslog",
			allowed: true,
		},
		{
			name:    "jenderal app log",
			path:    "/var/log/jenderal/app.log",
			allowed: true,
		},
		{
			name:    "etc passwd rejected",
			path:    "/etc/passwd",
			allowed: false,
		},
		{
			name:    "path traversal with ..",
			path:    "/var/log/nginx/../../../etc/passwd",
			allowed: false,
		},
		{
			name:    "double dot in middle",
			path:    "/var/log/../secret",
			allowed: false,
		},
		{
			name:    "arbitrary var log file",
			path:    "/var/log/auth.log",
			allowed: false,
		},
		{
			name:    "syslog with trailing slash",
			path:    "/var/log/syslog/",
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAllowedLogPath(tt.path)
			if got != tt.allowed {
				t.Errorf("isAllowedLogPath(%q) = %v, want %v", tt.path, got, tt.allowed)
			}
		})
	}
}

func TestReadLog(t *testing.T) {
	expectedOutput := "line1\nline2\nline3\n"

	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stdout:   expectedOutput,
				ExitCode: 0,
			}, nil
		},
	}

	svc := NewLogService(mock)
	content, err := svc.ReadLog(context.Background(), "/var/log/nginx/access.log", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if content != expectedOutput {
		t.Errorf("expected %q, got %q", expectedOutput, content)
	}
}

func TestReadLog_InvalidPath(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			t.Fatal("RunSudo should not be called for invalid path")
			return nil, nil
		},
	}

	svc := NewLogService(mock)
	_, err := svc.ReadLog(context.Background(), "/etc/passwd", 100)
	if err == nil {
		t.Fatal("expected error for invalid path /etc/passwd")
	}

	domainErr, ok := err.(*model.DomainError)
	if !ok {
		t.Fatalf("expected DomainError, got %T", err)
	}
	if domainErr.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %q", domainErr.Code)
	}
	if !strings.Contains(domainErr.Message, "/etc/passwd") {
		t.Errorf("expected message to mention /etc/passwd, got %q", domainErr.Message)
	}
}
