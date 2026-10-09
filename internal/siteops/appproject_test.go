package siteops

import (
	"context"
	"errors"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestAppProjectRoot_LegacyPublicGit(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" && len(args) == 2 && args[0] == "-d" && args[1] == "/home/u/public/.git" {
				return &executor.Result{ExitCode: 0}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		},
	}
	root, legacy := AppProjectRoot(context.Background(), mock, "/home/u", "/home/u/public")
	if root != "/home/u/public" {
		t.Fatalf("root = %q, want /home/u/public", root)
	}
	if !legacy {
		t.Fatal("legacy = false, want true")
	}
}

func TestAppProjectRoot_NewLayout(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 1}, nil
		},
	}
	root, legacy := AppProjectRoot(context.Background(), mock, "/home/u", "/home/u/app/public")
	if root != "/home/u/app" {
		t.Fatalf("root = %q, want /home/u/app", root)
	}
	if legacy {
		t.Fatal("legacy = true, want false")
	}
}

func TestAppProjectRoot_ExecErrorFallsBackToDocroot(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return nil, errors.New("executor unavailable")
		},
	}
	root, legacy := AppProjectRoot(context.Background(), mock, "/home/u", "/home/u/public")
	if root != "/home/u/public" {
		t.Fatalf("root = %q, want docroot /home/u/public", root)
	}
	if legacy {
		t.Fatal("legacy = true, want false")
	}
}
