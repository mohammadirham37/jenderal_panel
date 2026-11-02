package php

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestCLIDefaultAndSet(t *testing.T) {
	var sudo []string
	svc := NewService(&executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "readlink" {
				return &executor.Result{Stdout: "/usr/bin/php8.3\n"}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudo = append(sudo, name+" "+strings.Join(args, " "))
			if name == "test" && strings.HasSuffix(args[len(args)-1], "phar.phar8.2") {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{}, nil
		},
	}, nil)
	ctx := context.Background()

	if v, err := svc.CLIDefault(ctx); err != nil || v != "8.3" {
		t.Fatalf("CLIDefault = %q, %v", v, err)
	}
	if err := svc.SetCLIDefault(ctx, "8.2"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(sudo, "\n")
	for _, want := range []string{"update-alternatives --set php /usr/bin/php8.2", "update-alternatives --set phar /usr/bin/phar8.2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "--set phar.phar") {
		t.Error("a missing optional alternative must be skipped")
	}
	if err := svc.SetCLIDefault(ctx, "5.6"); err == nil {
		t.Error("an unsupported version must be refused")
	}
}
