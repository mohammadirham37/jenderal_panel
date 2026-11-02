package php

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func phpINIService(t *testing.T, phpOutput string) (*Service, *[]string) {
	t.Helper()
	var sudo []string
	return NewService(&executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if strings.HasPrefix(name, "/usr/bin/php") {
				return &executor.Result{Stderr: phpOutput}, nil
			}
			return &executor.Result{ExitCode: 1}, nil // os-release probe: default versions
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudo = append(sudo, name+" "+strings.Join(args, " "))
			return &executor.Result{Stdout: "memory_limit = 128M\n"}, nil
		},
	}, nil), &sudo
}

// The CLI php.ini (composer, artisan, cron) is edited without restarting
// PHP-FPM; the FPM one still restarts it.
func TestSavePHPINIWritesTheChosenSAPI(t *testing.T) {
	svc, sudo := phpINIService(t, "")
	if err := svc.SavePHPINI(context.Background(), "8.3", SAPICLI, "memory_limit = -1\n"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*sudo, "\n")
	if !strings.Contains(joined, " /etc/php/8.3/cli/php.ini") || strings.Contains(joined, "restart") {
		t.Errorf("cli save: %s", joined)
	}

	svc, sudo = phpINIService(t, "")
	if err := svc.SavePHPINI(context.Background(), "8.3", SAPIFPM, "memory_limit = 256M\n"); err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(*sudo, "\n")
	if !strings.Contains(joined, " /etc/php/8.3/fpm/php.ini") || !strings.Contains(joined, "systemctl restart php8.3-fpm") {
		t.Errorf("fpm save: %s", joined)
	}

	if content, err := svc.GetPHPINI(context.Background(), "8.3", SAPICLI); err != nil || content == "" {
		t.Errorf("GetPHPINI cli: %q %v", content, err)
	}
	if _, err := svc.GetPHPINI(context.Background(), "8.3", "apache2"); err == nil {
		t.Error("an unknown SAPI must be refused")
	}
}

func TestSavePHPINIRefusesAFileTheBinaryCannotParse(t *testing.T) {
	svc, sudo := phpINIService(t, "PHP:  syntax error, unexpected '=' in /tmp/jenderal-php-ini-1.ini on line 3")
	if err := svc.SavePHPINI(context.Background(), "8.3", SAPICLI, "memory_limit == 1\n"); err == nil {
		t.Fatal("a php.ini with a syntax error must be refused")
	}
	for _, call := range *sudo {
		if strings.HasPrefix(call, "cp ") {
			t.Errorf("the broken file was written: %s", call)
		}
	}
}
