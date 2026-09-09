package dependency

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestComposerStatusReportsMissingExecutableAsNotInstalled(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return nil, exec.ErrNotFound
		},
	}

	status, err := NewService(mock).ComposerStatus(context.Background())
	if err != nil {
		t.Fatalf("ComposerStatus() error = %v", err)
	}
	if status.Installed || status.Version != "" || status.Name != "composer" {
		t.Fatalf("status = %+v, want missing Composer", status)
	}
}

func TestComposerStatusParsesInstalledVersion(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name != "/usr/local/bin/composer" || strings.Join(args, " ") != "--version --no-ansi" {
				t.Fatalf("command = %q %q", name, args)
			}
			return &executor.Result{ExitCode: 0, Stdout: "Composer version 2.10.3 2026-08-27 15:41:32\n"}, nil
		},
	}

	status, err := NewService(mock).ComposerStatus(context.Background())
	if err != nil {
		t.Fatalf("ComposerStatus() error = %v", err)
	}
	if !status.Installed || status.Version != "2.10.3" {
		t.Fatalf("status = %+v, want installed Composer 2.10.3", status)
	}
}

func TestComposerInstallPlanVerifiesSignatureBeforeAtomicPromotion(t *testing.T) {
	commands := NewService(nil).ComposerInstallCommands()
	if len(commands) != 1 || len(commands[0]) != 3 || commands[0][0] != "bash" || commands[0][1] != "-c" {
		t.Fatalf("commands = %#v, want one fixed bash script", commands)
	}
	script := commands[0][2]
	checks := []string{
		"https://composer.github.io/installer.sig",
		"https://getcomposer.org/installer",
		"hash_file('sha384'",
		"test \"$actual\" = \"$expected\"",
		"--install-dir=\"$work_dir\" --filename=composer",
		"/usr/local/bin/composer.new",
		"mv -f /usr/local/bin/composer.new /usr/local/bin/composer",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Fatalf("installer missing %q:\n%s", check, script)
		}
	}
	verifyAt := strings.Index(script, `test "$actual" = "$expected"`)
	runAt := strings.Index(script, `php "$work_dir/composer-setup.php"`)
	promoteAt := strings.Index(script, "mv -f /usr/local/bin/composer.new /usr/local/bin/composer")
	if verifyAt < 0 || runAt < verifyAt || promoteAt < runAt {
		t.Fatalf("installer must verify, run, then promote:\n%s", script)
	}
}

func TestComposerUpdatePlanUsesStableComposerTwo(t *testing.T) {
	commands := NewService(nil).ComposerUpdateCommands()
	want := [][]string{
		{"/usr/local/bin/composer", "self-update", "--2", "--no-interaction", "--no-ansi"},
		{"/usr/local/bin/composer", "--version", "--no-ansi"},
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	for i := range want {
		if strings.Join(commands[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Fatalf("command %d = %#v, want %#v", i, commands[i], want[i])
		}
	}
}
