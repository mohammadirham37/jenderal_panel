package sshaccount

import (
	"context"
	"database/sql"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/database"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestValidateUsername(t *testing.T) {
	cases := map[string]bool{
		"budi":        true,
		"a":           true,
		"user_1":      true,
		"a-b_c":       true,
		"_lead":       true,
		"":            false,
		"1abc":        false,
		"-abc":        false,
		"UpperCase":   false,
		"web_example": false,
		"has space":   false,
		"sixtyfourcharssixtyfourcharssixtyfourcharssixtyfourcharsoverflow": false,
	}
	for name, want := range cases {
		if got := ValidateUsername(name); got != want {
			t.Errorf("ValidateUsername(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestAuthorizedKeysContent(t *testing.T) {
	keys := []model.SSHKey{
		{PublicKey: "ssh-ed25519 AAAAC3Nza budi@laptop"},
		{PublicKey: "  ssh-rsa AAAAB3Nza budi@desktop  "},
	}
	content := authorizedKeysContent(keys)
	lines := strings.Split(content, "\n")
	if len(lines) != 3 || lines[2] != "" {
		t.Fatalf("content = %q, want two keys with trailing newline", content)
	}
	if lines[0] != "ssh-ed25519 AAAAC3Nza budi@laptop" {
		t.Errorf("first line = %q, want trimmed key", lines[0])
	}
	if lines[1] != "ssh-rsa AAAAB3Nza budi@desktop" {
		t.Errorf("second line = %q, want trimmed key", lines[1])
	}
	if authorizedKeysContent(nil) != "" {
		t.Error("empty key list should render empty file")
	}
}

func TestFingerprintKeyRejectsMalformedInput(t *testing.T) {
	called := false
	svc := &Service{exec: &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		called = true
		return &executor.Result{ExitCode: 0, Stdout: "256 SHA256:abc comment (ED25519)"}, nil
	}}}

	for _, key := range []string{
		"",
		"not a key",
		"ssh-rsa",
		"command=\"rm -rf /\" ssh-rsa AAAAB3Nza",
		"no-pty ssh-ed25519 AAAAC3Nza",
		"ssh-ed25519 AAAAB2not/base64!! key",
		"ssh-ed25519 AAAAC3Nza extra1 extra2",
	} {
		if _, err := svc.fingerprintKey(context.Background(), key); err == nil {
			t.Errorf("fingerprintKey(%q) expected rejection", key)
		}
	}
	if called {
		t.Error("ssh-keygen should not run for structurally invalid keys")
	}
}

func TestFingerprintKeyUsesServerSideMetadata(t *testing.T) {
	svc := &Service{exec: &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		if name != "ssh-keygen" {
			t.Fatalf("expected ssh-keygen, got %s", name)
		}
		return &executor.Result{ExitCode: 0, Stdout: "3072 SHA256:ABCDEFG comment (RSA)\n"}, nil
	}}}

	key, err := svc.fingerprintKey(context.Background(), "ssh-rsa AAAAB3NzaC1 budi@host")
	if err != nil {
		t.Fatalf("fingerprintKey: %v", err)
	}
	if key.Fingerprint != "SHA256:ABCDEFG" {
		t.Errorf("fingerprint = %q", key.Fingerprint)
	}
	if key.Algo != "RSA" || key.Bits != 3072 {
		t.Errorf("algo/bits = %s/%d", key.Algo, key.Bits)
	}
}

func TestFingerprintKeyRejectsShortRSA(t *testing.T) {
	svc := &Service{exec: &executor.MockExecutor{RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		return &executor.Result{ExitCode: 0, Stdout: "1024 SHA256:SHORT k (RSA)"}, nil
	}}}
	if _, err := svc.fingerprintKey(context.Background(), "ssh-rsa AAAAB3NzaC1 short"); err == nil {
		t.Error("expected rejection of RSA keys below 2048 bits")
	}
}

func TestSanitizeKeyName(t *testing.T) {
	if got := sanitizeKeyName("  laptop \x07 key "); got != "laptop  key" {
		t.Errorf("sanitizeKeyName = %q", got)
	}
	long := strings.Repeat("x", 150)
	if got := sanitizeKeyName(long); len(got) != 100 {
		t.Errorf("sanitizeKeyName length = %d, want 100", len(got))
	}
}

// fakeSSHExecutor dispatches system commands with stateful behaviour: the
// Linux account exists only after a successful useradd.
type fakeSSHExecutor struct {
	exists    bool
	useraddN  int
	usermodN  int
	missingOK bool
}

func (f *fakeSSHExecutor) Run(ctx context.Context, name string, args ...string) (*executor.Result, error) {
	return f.RunSudo(ctx, name, args...)
}

func (f *fakeSSHExecutor) RunSudo(ctx context.Context, name string, args ...string) (*executor.Result, error) {
	switch name {
	case "id":
		if f.exists {
			return &executor.Result{ExitCode: 0}, nil
		}
		return &executor.Result{ExitCode: 1}, nil
	case "useradd":
		f.useraddN++
		f.exists = true
		return &executor.Result{ExitCode: 0}, nil
	case "usermod":
		f.usermodN++
		return &executor.Result{ExitCode: 0}, nil
	case "cat", "mkdir", "setfacl", "getfacl", "tee", "chown", "rm", "touch":
		return &executor.Result{ExitCode: 0}, nil
	default:
		return &executor.Result{ExitCode: 0}, nil
	}
}

func (f *fakeSSHExecutor) RunSudoWithInput(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
	return f.RunSudo(ctx, name, args...)
}

func (f *fakeSSHExecutor) RunSudoStream(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (f *fakeSSHExecutor) RunSudoWithInputStream(ctx context.Context, stdin io.Reader, stderrW io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func setupSSHTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`INSERT INTO users (id, username, email, password, is_active, ssh_enabled, created_at, updated_at)
		 VALUES ('u-admin', 'admin', 'admin@test', 'x', 1, 1, ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return db
}

func TestLockToleratesMissingSystemAccount(t *testing.T) {
	db := setupSSHTestDB(t)
	fake := &fakeSSHExecutor{}
	svc := NewService(db, fake, audit.NewService(setupSSHTestDB(t)))

	if err := svc.Lock(context.Background(), "u-admin"); err != nil {
		t.Fatalf("Lock with missing system account must succeed: %v", err)
	}
	if fake.usermodN != 0 {
		t.Errorf("usermod must not run without a system account, ran %d times", fake.usermodN)
	}
}

func TestResumeProvisionsMissingSystemAccount(t *testing.T) {
	db := setupSSHTestDB(t)
	fake := &fakeSSHExecutor{}
	svc := NewService(db, fake, audit.NewService(setupSSHTestDB(t)))

	if err := svc.Resume(context.Background(), "u-admin"); err != nil {
		t.Fatalf("Resume with missing system account must self-heal: %v", err)
	}
	if fake.useraddN == 0 {
		t.Error("Resume must provision the missing account")
	}
	if fake.exists == false {
		t.Error("account should exist after provisioning")
	}
}

func TestSyncOwnedWebsitesSkipsMissingSystemAccount(t *testing.T) {
	db := setupSSHTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, created_by, created_at, updated_at)
		 VALUES ('w-1', 'example.com', 'php', '8.2', '/home/web_example_com/public', 'web_example_com', 'active', 'u-admin', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed website: %v", err)
	}

	fake := &fakeSSHExecutor{}
	svc := NewService(db, fake, audit.NewService(setupSSHTestDB(t)))

	if err := svc.SyncOwnedWebsites(context.Background(), "u-admin"); err != nil {
		t.Fatalf("SyncOwnedWebsites: %v", err)
	}
	if fake.usermodN != 0 {
		t.Errorf("ACL usermod must not run without a system account, ran %d times", fake.usermodN)
	}
}
