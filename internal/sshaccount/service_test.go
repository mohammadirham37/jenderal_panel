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
	exists        bool
	useraddN      int
	usermodN      int
	missingOK     bool
	chpasswdN     int
	chpasswdInput string
	aclInstalled  bool
	setfaclPre    bool
	canEnter      bool
	chmodN        int
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
	case "apt-get":
		for _, arg := range args {
			if arg == "acl" {
				f.aclInstalled = true
			}
		}
		return &executor.Result{ExitCode: 0}, nil
	case "setfacl", "getfacl":
		if f.aclInstalled || f.setfaclPre {
			return &executor.Result{ExitCode: 0}, nil
		}
		return &executor.Result{ExitCode: 1, Stderr: name + ": command not found"}, nil
	case "chmod":
		f.chmodN++
		return &executor.Result{ExitCode: 0}, nil
	case "-u":
		// sudo -u <account> -- /usr/bin/test -x <dir>: the traversal check
		// for that account.
		for _, arg := range args {
			if arg == "/usr/bin/test" {
				if f.canEnter {
					return &executor.Result{ExitCode: 0}, nil
				}
				return &executor.Result{ExitCode: 1}, nil
			}
		}
		return &executor.Result{ExitCode: 0}, nil
	case "test", "/usr/bin/test":
		if f.canEnter {
			return &executor.Result{ExitCode: 0}, nil
		}
		return &executor.Result{ExitCode: 1}, nil
	case "cat", "mkdir", "tee", "chown", "rm", "touch":
		return &executor.Result{ExitCode: 0}, nil
	default:
		return &executor.Result{ExitCode: 0}, nil
	}
}

func (f *fakeSSHExecutor) RunSudoWithInput(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
	if name == "chpasswd" {
		f.chpasswdN++
		f.chpasswdInput = input
	}
	return f.RunSudo(ctx, name, args...)
}

func (f *fakeSSHExecutor) RunSudoStream(ctx context.Context, w io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (f *fakeSSHExecutor) RunSudoStreamSplit(context.Context, io.Writer, io.Writer, string, ...string) (int, error) {
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

func TestSetPasswordMirrorsPanelPasswordOntoAccount(t *testing.T) {
	db := setupSSHTestDB(t)
	fake := &fakeSSHExecutor{}
	svc := NewService(db, fake, audit.NewService(setupSSHTestDB(t)))

	if err := svc.Provision(context.Background(), "u-admin"); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := svc.SetPassword(context.Background(), "u-admin", "s3cret pa:ss"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if fake.chpasswdN != 1 {
		t.Fatalf("chpasswd ran %d times, want 1", fake.chpasswdN)
	}
	if fake.chpasswdInput != "admin:s3cret pa:ss\n" {
		t.Errorf("chpasswd input = %q, want %q", fake.chpasswdInput, "admin:s3cret pa:ss\n")
	}
}

func TestSetPasswordSkipsWithoutAccountOrSSH(t *testing.T) {
	db := setupSSHTestDB(t)
	if _, err := db.Exec(`UPDATE users SET ssh_enabled = 0 WHERE id = 'u-admin'`); err != nil {
		t.Fatalf("disable ssh: %v", err)
	}
	fake := &fakeSSHExecutor{}
	svc := NewService(db, fake, audit.NewService(setupSSHTestDB(t)))

	if err := svc.SetPassword(context.Background(), "u-admin", "secret"); err != nil {
		t.Fatalf("SetPassword with ssh disabled: %v", err)
	}
	if fake.chpasswdN != 0 {
		t.Errorf("chpasswd must not run with ssh disabled, ran %d times", fake.chpasswdN)
	}

	if _, err := db.Exec(`UPDATE users SET ssh_enabled = 1 WHERE id = 'u-admin'`); err != nil {
		t.Fatalf("enable ssh: %v", err)
	}
	if err := svc.SetPassword(context.Background(), "u-admin", "secret"); err != nil {
		t.Fatalf("SetPassword with missing account: %v", err)
	}
	if fake.chpasswdN != 0 {
		t.Errorf("chpasswd must not run without a system account, ran %d times", fake.chpasswdN)
	}

	if err := svc.SetPassword(context.Background(), "u-admin", ""); err == nil {
		t.Error("expected rejection of empty password")
	}
	if err := svc.SetPassword(context.Background(), "u-admin", "line\nbreak"); err == nil {
		t.Error("expected rejection of password with line break")
	}
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

func TestReconcileAllSyncsOnlySSHEnabledUsers(t *testing.T) {
	db := setupSSHTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO users (id, username, email, password, is_active, ssh_enabled, created_at, updated_at)
		 VALUES ('u-user', 'budi', 'budi@test', 'x', 1, 0, ?, ?)`, now, now,
	); err != nil {
		t.Fatalf("seed disabled user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, created_by, created_at, updated_at)
		 VALUES ('w-1', 'example.com', 'php', '8.2', '/home/web_example_com/public', 'web_example_com', 'active', 'u-admin', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed website: %v", err)
	}

	fake := &fakeSSHExecutor{}
	svc := NewService(db, fake, audit.NewService(setupSSHTestDB(t)))
	if err := svc.Provision(context.Background(), "u-admin"); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	fake.usermodN = 0

	if err := svc.ReconcileAll(context.Background()); err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}
	if fake.usermodN == 0 {
		t.Error("ReconcileAll must re-grant site access for the SSH-enabled user")
	}

	if _, err := db.Exec(`UPDATE users SET ssh_enabled = 0`); err != nil {
		t.Fatalf("disable all users: %v", err)
	}
	fake.usermodN = 0
	if err := svc.ReconcileAll(context.Background()); err != nil {
		t.Fatalf("ReconcileAll with no enabled users: %v", err)
	}
	if fake.usermodN != 0 {
		t.Errorf("usermod ran %d times with no SSH-enabled users, want 0", fake.usermodN)
	}
}

func TestGrantWebsiteOwnerVerifiesTraverseAccess(t *testing.T) {
	db := setupSSHTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, created_by, created_at, updated_at)
		 VALUES ('w-1', 'example.com', 'php', '8.2', '/home/web_example_com/public', 'web_example_com', 'active', 'u-admin', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed website: %v", err)
	}
	fake := &fakeSSHExecutor{setfaclPre: true}
	svc := NewService(db, fake, audit.NewService(setupSSHTestDB(t)))
	if err := svc.Provision(context.Background(), "u-admin"); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	fake.canEnter = true
	if err := svc.GrantWebsiteOwner(context.Background(), "w-1"); err != nil {
		t.Fatalf("GrantWebsiteOwner: %v", err)
	}

	fake.canEnter = false
	err := svc.GrantWebsiteOwner(context.Background(), "w-1")
	if err == nil || !strings.Contains(err.Error(), "log out and log back in") {
		t.Fatalf("GrantWebsiteOwner without traversal = %v, want re-login guidance", err)
	}
}

func TestGrantWebsiteInstallsAclWhenMissing(t *testing.T) {
	db := setupSSHTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, created_by, created_at, updated_at)
		 VALUES ('w-2', 'acl.example.com', 'php', '8.2', '/home/web_acl_example/public', 'web_acl_example', 'active', 'u-admin', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("seed website: %v", err)
	}
	fake := &fakeSSHExecutor{}
	svc := NewService(db, fake, audit.NewService(setupSSHTestDB(t)))

	if err := svc.Provision(context.Background(), "u-admin"); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !fake.aclInstalled {
		t.Error("missing setfacl must trigger the acl package installation")
	}
	if fake.chmodN != 0 {
		t.Errorf("chmod fallback must not run once acl is installed, ran %d times", fake.chmodN)
	}
}
