package fail2ban

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

type fakeManagedFiles struct {
	mu      sync.Mutex
	content map[string]string
}

func newFakeManagedFiles(content map[string]string) *fakeManagedFiles {
	return &fakeManagedFiles{content: content}
}

func (f *fakeManagedFiles) Read(_ context.Context, path string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	content, ok := f.content[path]
	return content, ok, nil
}

func (f *fakeManagedFiles) WriteAtomic(_ context.Context, path, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.content[path] = content
	return nil
}

func (f *fakeManagedFiles) Remove(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.content, path)
	return nil
}

func (f *fakeManagedFiles) Content(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.content[path]
}

func TestApplyRestoresPreviousConfigWhenReloadFails(t *testing.T) {
	files := newFakeManagedFiles(map[string]string{managedConfigPath: "previous"})
	reloads := 0
	exec := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "mktemp" {
				return &executor.Result{Stdout: "/tmp/jenderal-fail2ban.test\n"}, nil
			}
			if name == "systemctl" && len(args) > 0 && args[0] == "reload" {
				reloads++
				if reloads == 1 {
					return &executor.Result{ExitCode: 1, Stderr: "reload failed"}, nil
				}
			}
			return &executor.Result{}, nil
		},
	}
	svc := NewService(exec, files, nil, nil)
	if err := svc.Apply(context.Background(), SafeSettings()); err == nil {
		t.Fatal("expected reload failure")
	}
	if got := files.Content(managedConfigPath); got != "previous" {
		t.Fatalf("config=%q", got)
	}
	if reloads != 2 {
		t.Fatalf("reload attempts = %d, want failed promotion and successful rollback", reloads)
	}
}

func TestStatusReportsMissingPackageWithoutError(t *testing.T) {
	exec := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 127}, nil
		},
	}
	status, err := NewService(exec, newFakeManagedFiles(map[string]string{}), nil, nil).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed || status.State != "not_installed" {
		t.Fatalf("status = %#v", status)
	}
}

func TestApplyRejectsUnavailableFilterBeforeWriting(t *testing.T) {
	files := newFakeManagedFiles(map[string]string{managedConfigPath: "previous"})
	exec := &executor.MockExecutor{
		RunSudoFunc: func(_ context.Context, name string, _ ...string) (*executor.Result, error) {
			if name == "test" {
				return &executor.Result{ExitCode: 1}, nil
			}
			return &executor.Result{}, nil
		},
	}
	err := NewService(exec, files, nil, nil).Apply(context.Background(), SafeSettings())
	if err == nil || !strings.Contains(err.Error(), "filter") {
		t.Fatalf("error = %v", err)
	}
	if got := files.Content(managedConfigPath); got != "previous" {
		t.Fatalf("config changed to %q", got)
	}
}

func TestApplyRejectsUnavailableLogSourceBeforeWriting(t *testing.T) {
	files := newFakeManagedFiles(map[string]string{managedConfigPath: "previous"})
	exec := &executor.MockExecutor{
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" && len(args) > 0 && args[0] == "-f" {
				return &executor.Result{}, nil
			}
			return &executor.Result{ExitCode: 1}, nil
		},
	}
	err := NewService(exec, files, nil, nil).Apply(context.Background(), SafeSettings())
	if err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("error = %v", err)
	}
	if got := files.Content(managedConfigPath); got != "previous" {
		t.Fatalf("config changed to %q", got)
	}
}

func TestApplyPromotesValidatedConfigAndConfirmsJail(t *testing.T) {
	files := newFakeManagedFiles(map[string]string{})
	exec := applyExecutor(func(name string, args ...string) *executor.Result {
		if name == "mktemp" {
			return &executor.Result{Stdout: "/tmp/jenderal-fail2ban.success\n"}
		}
		if name == "fail2ban-client" && len(args) == 1 && args[0] == "status" {
			return &executor.Result{Stdout: "Status\n`- Jail list: sshd\n"}
		}
		return &executor.Result{}
	})
	if err := NewService(exec, files, nil, nil).Apply(context.Background(), SafeSettings()); err != nil {
		t.Fatal(err)
	}
	if got := files.Content(managedConfigPath); !strings.Contains(got, "[sshd]") {
		t.Fatalf("promoted config = %q", got)
	}
}

func TestApplyDoesNotPromoteInvalidCandidate(t *testing.T) {
	files := newFakeManagedFiles(map[string]string{managedConfigPath: "previous"})
	exec := applyExecutor(func(name string, args ...string) *executor.Result {
		if name == "mktemp" {
			return &executor.Result{Stdout: "/tmp/jenderal-fail2ban.invalid\n"}
		}
		if name == "fail2ban-client" && len(args) > 0 && args[0] == "-t" {
			return &executor.Result{ExitCode: 1, Stderr: "invalid jail"}
		}
		return &executor.Result{}
	})
	if err := NewService(exec, files, nil, nil).Apply(context.Background(), SafeSettings()); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	if got := files.Content(managedConfigPath); got != "previous" {
		t.Fatalf("config changed to %q", got)
	}
}

func TestManualBanPersistsAndReconcilesRequestedExpiry(t *testing.T) {
	db := migratedFail2banDB(t)
	var now = time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)
	unbans := 0
	exec := &executor.MockExecutor{
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "fail2ban-client" && equalStrings(args, []string{"status"}) {
				return &executor.Result{Stdout: "Jail list: sshd"}, nil
			}
			if name == "fail2ban-client" && equalStrings(args, []string{"get", "sshd", "bantime"}) {
				return &executor.Result{Stdout: "900\n"}, nil
			}
			if name == "fail2ban-client" && len(args) == 4 && args[2] == "unbanip" {
				unbans++
			}
			return &executor.Result{}, nil
		},
	}
	svc := NewService(exec, newFakeManagedFiles(map[string]string{}), db, nil)
	svc.now = func() time.Time { return now }
	ban, err := svc.Ban(context.Background(), "sshd", "203.0.113.7", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ban.ExpiresAt == nil || !ban.ExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("actual expiry = %v", ban.ExpiresAt)
	}
	now = now.Add(6 * time.Minute)
	if err := svc.ReconcileExpiredBans(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM security_manual_bans WHERE id = ?`, ban.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "released" || unbans != 1 {
		t.Fatalf("status=%q unbans=%d", status, unbans)
	}
}

func TestManualBanRejectsInactiveAllowlistedJail(t *testing.T) {
	exec := &executor.MockExecutor{RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "fail2ban-client" && equalStrings(args, []string{"status"}) {
			return &executor.Result{Stdout: "Jail list: nginx-http-auth"}, nil
		}
		return &executor.Result{Stdout: "900"}, nil
	}}
	_, err := NewService(exec, newFakeManagedFiles(map[string]string{}), nil, nil).
		Ban(context.Background(), "sshd", "203.0.113.7", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("error = %v", err)
	}
}

func TestManualBanRejectsUnknownJailWithoutCommand(t *testing.T) {
	calls := 0
	exec := &executor.MockExecutor{RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
		calls++
		return &executor.Result{}, nil
	}}
	_, err := NewService(exec, newFakeManagedFiles(map[string]string{}), nil, nil).
		Ban(context.Background(), "../../sshd", "203.0.113.7", time.Minute)
	if err == nil || calls != 0 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestInstallUsesBoundedAptOptionsAndEnablesService(t *testing.T) {
	var calls [][]string
	exec := &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			calls = append(calls, append([]string{name}, args...))
			return &executor.Result{}, nil
		},
	}
	svc := NewService(exec, newFakeManagedFiles(map[string]string{}), nil, nil)
	if err := svc.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantApt := []string{"apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120", "-o", "Acquire::Retries=2", "-o", "Acquire::http::Timeout=30", "-o", "Acquire::https::Timeout=30", "fail2ban"}
	if len(calls) != 2 || !equalStrings(calls[0], wantApt) || !equalStrings(calls[1], []string{"systemctl", "enable", "--now", "fail2ban"}) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestApplyReportsFileFailure(t *testing.T) {
	files := &errorManagedFiles{err: errors.New("disk full")}
	svc := NewService(successExecutor(), files, nil, nil)
	if err := svc.Apply(context.Background(), SafeSettings()); err == nil {
		t.Fatal("expected managed file failure")
	}
}

type errorManagedFiles struct{ err error }

func (f *errorManagedFiles) Read(context.Context, string) (string, bool, error) {
	return "", false, f.err
}
func (f *errorManagedFiles) WriteAtomic(context.Context, string, string) error { return f.err }
func (f *errorManagedFiles) Remove(context.Context, string) error              { return f.err }

func successExecutor() executor.CommandExecutor {
	return &executor.MockExecutor{
		RunFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(context.Context, string, ...string) (*executor.Result, error) {
			return &executor.Result{Stdout: "/tmp/jenderal-fail2ban.test\n"}, nil
		},
		RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
			return &executor.Result{}, nil
		},
	}
}

func applyExecutor(result func(string, ...string) *executor.Result) executor.CommandExecutor {
	return &executor.MockExecutor{
		RunFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			return result(name, args...), nil
		},
		RunSudoFunc: func(_ context.Context, name string, args ...string) (*executor.Result, error) {
			return result(name, args...), nil
		},
		RunSudoWithInputFunc: func(_ context.Context, _ string, name string, args ...string) (*executor.Result, error) {
			return result(name, args...), nil
		},
	}
}

func migratedFail2banDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := database.Migrate(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
