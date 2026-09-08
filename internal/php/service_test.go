package php

import (
	"context"
	"errors"
	osexec "os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// mockResult creates an executor.Result with the given stdout, stderr, and exit code.
func mockResult(stdout, stderr string, exitCode int) *executor.Result {
	return &executor.Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
		Duration: time.Millisecond,
	}
}

func TestListInstalled(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// "test -d /etc/php/{ver}"
			if name == "test" && len(args) >= 2 && args[0] == "-d" {
				dir := args[1]
				// Only 8.3 and 8.4 are installed.
				if strings.HasSuffix(dir, "/8.3") || strings.HasSuffix(dir, "/8.4") {
					return mockResult("", "", 0), nil
				}
				return mockResult("", "", 1), nil
			}
			// "systemctl show ..."
			if name == "systemctl" {
				for _, a := range args {
					if strings.Contains(a, "php8.4-fpm") {
						return mockResult("ActiveState=active\nUnitFileState=enabled\n", "", 0), nil
					}
					if strings.Contains(a, "php8.3-fpm") {
						return mockResult("ActiveState=inactive\nUnitFileState=disabled\n", "", 0), nil
					}
				}
			}
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
	}

	svc := NewService(mock, nil)
	versions, err := svc.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 4 {
		t.Fatalf("expected 4 versions, got %d", len(versions))
	}

	// Build a map for easier assertion.
	vmap := make(map[string]model.PHPVersion)
	for _, v := range versions {
		vmap[v.Version] = v
	}

	// 8.1 and 8.2 should not be installed.
	if vmap["8.1"].Installed {
		t.Error("expected 8.1 not installed")
	}
	if vmap["8.2"].Installed {
		t.Error("expected 8.2 not installed")
	}

	// 8.3 should be installed but not running.
	v83 := vmap["8.3"]
	if !v83.Installed {
		t.Error("expected 8.3 installed")
	}
	if v83.Running {
		t.Error("expected 8.3 not running")
	}
	if v83.Enabled {
		t.Error("expected 8.3 not enabled")
	}

	// 8.4 should be installed, running, and enabled.
	v84 := vmap["8.4"]
	if !v84.Installed {
		t.Error("expected 8.4 installed")
	}
	if !v84.Running {
		t.Error("expected 8.4 running")
	}
	if !v84.Enabled {
		t.Error("expected 8.4 enabled")
	}
}

func TestValidateVersion(t *testing.T) {
	svc := NewService(nil, nil)

	tests := []struct {
		version string
		wantErr bool
	}{
		{"8.1", false},
		{"8.2", false},
		{"8.3", false},
		{"8.4", false},
		{"7.4", true},
		{"abc", true},
		{"", true},
	}

	for _, tc := range tests {
		err := svc.validateVersion(tc.version)
		if tc.wantErr && err == nil {
			t.Errorf("validateVersion(%q): expected error, got nil", tc.version)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("validateVersion(%q): unexpected error: %v", tc.version, err)
		}
	}
}

func TestInstall_ValidVersion(t *testing.T) {
	var capturedName string
	var capturedArgs []string

	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			capturedName = name
			capturedArgs = args
			return mockResult("", "", 0), nil
		},
	}

	svc := NewService(mock, nil)
	err := svc.Install(context.Background(), "8.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedName != "apt-get" {
		t.Errorf("expected command 'apt-get', got %q", capturedName)
	}

	// Verify the args contain install -y and the expected packages.
	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "install -y") {
		t.Error("expected args to contain 'install -y'")
	}
	expectedPkgs := []string{
		"php8.3-fpm", "php8.3-cli", "php8.3-common",
		"php8.3-mysql", "php8.3-pgsql", "php8.3-mbstring",
		"php8.3-xml", "php8.3-curl", "php8.3-zip",
		"php8.3-gd", "php8.3-intl", "php8.3-bcmath",
	}
	for _, pkg := range expectedPkgs {
		if !strings.Contains(argsStr, pkg) {
			t.Errorf("expected args to contain %q", pkg)
		}
	}
}

func TestInstallConfiguresPPADirectlyWithoutLaunchpadAPI(t *testing.T) {
	type command struct {
		name string
		args []string
	}
	var commands []command
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			commands = append(commands, command{name: name, args: append([]string(nil), args...)})
			return mockResult("", "", 0), nil
		},
	}

	svc := NewService(mock, nil)
	if err := svc.Install(context.Background(), "8.3"); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	var repositoryScript string
	for _, cmd := range commands {
		if cmd.name == "add-apt-repository" {
			t.Fatal("Install() still depends on the Launchpad API through add-apt-repository")
		}
		if cmd.name == "bash" && len(cmd.args) == 2 && cmd.args[0] == "-c" {
			repositoryScript = cmd.args[1]
		}
		if cmd.name == "apt-get" {
			args := strings.Join(cmd.args, " ")
			if !strings.Contains(args, "Acquire::https::Timeout=30") {
				t.Errorf("apt command has no stalled HTTPS connection timeout: apt-get %s", args)
			}
		}
	}
	if repositoryScript == "" {
		t.Fatal("Install() did not configure the PHP repository directly")
	}

	for _, required := range []string{
		"https://ppa.launchpadcontent.net/ondrej/php/ubuntu",
		"B8DC7E53946656EFBCE4C1DD71DAEAAB4AD4CAB6",
		"signed-by=/usr/share/keyrings/ondrej-php.gpg",
		"--connect-timeout 10",
		"--max-time 60",
		"primary_key_count",
		"ondrej-ubuntu-php-*.sources",
	} {
		if !strings.Contains(repositoryScript, required) {
			t.Errorf("repository setup script missing %q", required)
		}
	}
	if strings.Index(repositoryScript, ". /etc/os-release") > strings.Index(repositoryScript, "curl --fail") {
		t.Error("repository setup downloads a key before validating the Ubuntu codename")
	}
}

func TestPHPRepositorySetupScriptIsValidBash(t *testing.T) {
	cmd := osexec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(phpRepositorySetupScript)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("repository setup script has invalid bash syntax: %v\n%s", err, output)
	}
}

func TestInstallAllowsBoundedTimeForAptSteps(t *testing.T) {
	var shortestDeadline time.Duration
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Errorf("%s has no explicit deadline", name)
			} else {
				remaining := time.Until(deadline)
				if shortestDeadline == 0 || remaining < shortestDeadline {
					shortestDeadline = remaining
				}
			}
			return mockResult("", "", 0), nil
		},
	}

	if err := NewService(mock, nil).Install(context.Background(), "8.3"); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if shortestDeadline < 14*time.Minute {
		t.Fatalf("shortest install step deadline = %s, want approximately 15 minutes", shortestDeadline)
	}
}

func TestInstall_InvalidVersion(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return mockResult("", "", 0), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			t.Fatal("RunSudo should not be called for invalid version")
			return nil, nil
		},
	}

	svc := NewService(mock, nil)
	err := svc.Install(context.Background(), "7.0")
	if err == nil {
		t.Fatal("expected error for invalid version, got nil")
	}

	var domainErr *model.DomainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if domainErr.Code != "VALIDATION_ERROR" {
		t.Errorf("expected code VALIDATION_ERROR, got %q", domainErr.Code)
	}
}

func TestParseFPMStatus(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantRunning bool
		wantEnabled bool
	}{
		{
			name:        "active and enabled",
			output:      "ActiveState=active\nUnitFileState=enabled\n",
			wantRunning: true,
			wantEnabled: true,
		},
		{
			name:        "inactive and disabled",
			output:      "ActiveState=inactive\nUnitFileState=disabled\n",
			wantRunning: false,
			wantEnabled: false,
		},
		{
			name:        "active but disabled",
			output:      "ActiveState=active\nUnitFileState=disabled\n",
			wantRunning: true,
			wantEnabled: false,
		},
		{
			name:        "empty output",
			output:      "",
			wantRunning: false,
			wantEnabled: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			running, enabled := parseFPMStatus(tc.output)
			if running != tc.wantRunning {
				t.Errorf("running: got %v, want %v", running, tc.wantRunning)
			}
			if enabled != tc.wantEnabled {
				t.Errorf("enabled: got %v, want %v", enabled, tc.wantEnabled)
			}
		})
	}
}
