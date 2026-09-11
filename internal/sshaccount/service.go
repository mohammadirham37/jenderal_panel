// Package sshaccount provisions panel users as Linux SSH accounts and keeps
// their access to the websites they own in sync. Authentication is public key
// only: accounts are created with a locked password and the authorized_keys
// file is always rewritten from the panel database.
package sshaccount

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// usernameRegex mirrors useradd(8) NAME_REGEX with a 32 character cap.
var usernameRegex = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// webUserRegex matches the site system users the website module creates.
var webUserRegex = regexp.MustCompile(`^web_[a-z0-9_-]+$`)

// maxKeysPerUser caps the authorized_keys file size per account.
const maxKeysPerUser = 20

// ErrInvalidUsername reports a panel username that cannot double as a Linux
// account name.
var ErrInvalidUsername = model.NewValidationError("username must be 1-32 characters of a-z, 0-9, underscore, or hyphen and cannot start with a digit or hyphen")

// Service provisions panel users as SSH-capable Linux accounts.
type Service struct {
	db    *sql.DB
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new sshaccount Service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc}
}

// ValidateUsername reports whether username can be used as a Linux account.
// The web_ prefix is reserved for website system users.
func ValidateUsername(username string) bool {
	return usernameRegex.MatchString(username) && !strings.HasPrefix(username, "web_")
}

// HomeDir returns the system home directory for a panel username.
func HomeDir(username string) string {
	return "/home/" + username
}

// userRecord is the minimal user information the module needs.
type userRecord struct {
	ID         string
	Username   string
	SSHEnabled bool
}

func (s *Service) loadUser(ctx context.Context, userID string) (userRecord, error) {
	var u userRecord
	var enabled int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, ssh_enabled FROM users WHERE id = ?`, userID,
	).Scan(&u.ID, &u.Username, &enabled)
	if err == sql.ErrNoRows {
		return userRecord{}, model.ErrNotFound
	}
	if err != nil {
		return userRecord{}, fmt.Errorf("load user: %w", err)
	}
	u.SSHEnabled = enabled == 1
	return u, nil
}

// accountExists reports whether a Linux account with this name exists.
func (s *Service) accountExists(ctx context.Context, username string) (bool, error) {
	result, err := s.exec.RunSudo(ctx, "id", "-u", username)
	if err != nil {
		return false, err
	}
	switch result.ExitCode {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf("check account %s: %s", username, strings.TrimSpace(result.Stderr))
	}
}

// Provision creates the Linux account for a panel user: home directory under
// /home, bash shell, locked password (key-only login), and read/write access
// to every website the user owns. An existing account is accepted so retries
// converge instead of failing.
func (s *Service) Provision(ctx context.Context, userID string) error {
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return err
	}
	if !ValidateUsername(u.Username) {
		return ErrInvalidUsername
	}

	result, err := s.exec.RunSudo(ctx, "useradd",
		"--create-home",
		"--home-dir", HomeDir(u.Username),
		"--shell", "/bin/bash",
		u.Username,
	)
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	// Exit code 9 means the username is already in use; treat the existing
	// account as provisioned so re-enabling SSH stays idempotent.
	if result.ExitCode != 0 && result.ExitCode != 9 {
		return fmt.Errorf("create account %s: %s", u.Username, strings.TrimSpace(result.Stderr))
	}
	if err := s.runSudoOK(ctx, "usermod", "-p", "*", u.Username); err != nil {
		return fmt.Errorf("lock account password: %w", err)
	}

	if err := s.SyncOwnedWebsites(ctx, userID); err != nil {
		return err
	}

	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_account_provision", Module: "ssh", Target: u.Username, Detail: "provisioned SSH account " + u.Username})
	return nil
}

// Lock disables shell login while preserving the account and its files.
func (s *Service) Lock(ctx context.Context, userID string) error {
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return err
	}
	if !ValidateUsername(u.Username) {
		return ErrInvalidUsername
	}
	exists, err := s.accountExists(ctx, u.Username)
	if err != nil {
		return err
	}
	if !exists {
		// Nothing to lock: the panel user has no Linux account (legacy
		// users, or provisioning never completed). Already as good as
		// locked.
		return nil
	}
	if err := s.runSudoOK(ctx, "usermod", "-L", "-s", "/usr/sbin/nologin", u.Username); err != nil {
		return fmt.Errorf("lock account: %w", err)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_account_lock", Module: "ssh", Target: u.Username})
	return nil
}

// Resume re-enables shell login for a previously locked account.
func (s *Service) Resume(ctx context.Context, userID string) error {
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return err
	}
	if !ValidateUsername(u.Username) {
		return ErrInvalidUsername
	}
	exists, err := s.accountExists(ctx, u.Username)
	if err != nil {
		return err
	}
	if !exists {
		// The account was never provisioned (or was removed); create it so
		// the resumed state converges to a working SSH setup.
		return s.Provision(ctx, userID)
	}
	if err := s.runSudoOK(ctx, "usermod", "-U", "-s", "/bin/bash", u.Username); err != nil {
		return fmt.Errorf("unlock account: %w", err)
	}
	return nil
}

// Delete removes the Linux account and its home directory, then drops the
// stored SSH keys. A missing account is treated as success.
func (s *Service) Delete(ctx context.Context, userID string) error {
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil
		}
		return err
	}
	if !ValidateUsername(u.Username) {
		return ErrInvalidUsername
	}

	result, err := s.exec.RunSudo(ctx, "userdel", "-r", u.Username)
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	// Exit code 6 means the user does not exist; deleting again is fine.
	if result.ExitCode != 0 && result.ExitCode != 6 {
		return fmt.Errorf("delete account %s: %s", u.Username, strings.TrimSpace(result.Stderr))
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM user_ssh_keys WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete ssh keys: %w", err)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_account_delete", Module: "ssh", Target: u.Username, Detail: "deleted SSH account " + u.Username})
	return nil
}

// SyncOwnedWebsites grants the account access to every website currently
// owned by the panel user. Safe to run repeatedly.
func (s *Service) SyncOwnedWebsites(ctx context.Context, userID string) error {
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return err
	}
	if !u.SSHEnabled {
		return nil
	}
	if exists, err := s.accountExists(ctx, u.Username); err != nil {
		return err
	} else if !exists {
		// Without a Linux account there are no ACLs to converge; the caller
		// provisions first when SSH is actually wanted.
		return nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT web_user FROM websites WHERE created_by = ?`, userID)
	if err != nil {
		return fmt.Errorf("list owned websites: %w", err)
	}
	defer rows.Close()

	var webUsers []string
	for rows.Next() {
		var webUser string
		if err := rows.Scan(&webUser); err != nil {
			return fmt.Errorf("scan owned websites: %w", err)
		}
		webUsers = append(webUsers, webUser)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, webUser := range webUsers {
		if err := s.GrantWebsite(ctx, u.Username, webUser); err != nil {
			return err
		}
	}
	return nil
}

// GrantWebsite gives a panel account group membership plus POSIX ACL
// read/write access to a website's project directory. Falls back to
// group-write permissions when setfacl is unavailable.
func (s *Service) GrantWebsite(ctx context.Context, username, webUser string) error {
	if !ValidateUsername(username) || !webUserRegex.MatchString(webUser) {
		return model.NewValidationError("unsafe account or website user name")
	}
	if err := s.runSudoOK(ctx, "usermod", "-aG", webUser, username); err != nil {
		return fmt.Errorf("join website group: %w", err)
	}
	siteHome := "/home/" + webUser
	if s.setfaclAvailable(ctx) {
		if err := s.runSudoOK(ctx, "setfacl", "-R", "-m", "u:"+username+":rwX", siteHome); err != nil {
			return fmt.Errorf("grant site ACL: %w", err)
		}
		if err := s.runSudoOK(ctx, "setfacl", "-R", "-d", "-m", "u:"+username+":rwX", siteHome); err != nil {
			return fmt.Errorf("grant default site ACL: %w", err)
		}
		return nil
	}
	return s.runSudoOK(ctx, "chmod", "-R", "g+rwX", siteHome)
}

// RevokeWebsite removes a panel account's access to a website after an
// ownership transfer. Best effort: already-revoked access is not an error.
func (s *Service) RevokeWebsite(ctx context.Context, username, webUser string) error {
	if !ValidateUsername(username) || !webUserRegex.MatchString(webUser) {
		return model.NewValidationError("unsafe account or website user name")
	}
	siteHome := "/home/" + webUser
	if s.setfaclAvailable(ctx) {
		_, _ = s.exec.RunSudo(ctx, "setfacl", "-R", "-x", "u:"+username, siteHome)
		_, _ = s.exec.RunSudo(ctx, "setfacl", "-R", "-d", "-x", "u:"+username, siteHome)
	}
	_, _ = s.exec.RunSudo(ctx, "gpasswd", "-d", username, webUser)
	return nil
}

func (s *Service) setfaclAvailable(ctx context.Context) bool {
	result, err := s.exec.Run(ctx, "setfacl", "--version")
	return err == nil && result != nil && result.ExitCode == 0
}

func (s *Service) runSudoOK(ctx context.Context, name string, args ...string) error {
	result, err := s.exec.RunSudo(ctx, name, args...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = fmt.Sprintf("exit status %d", result.ExitCode)
		}
		return fmt.Errorf("%s: %s", name, message)
	}
	return nil
}

// authorizedKeysContent renders the file body for a key list.
func authorizedKeysContent(keys []model.SSHKey) string {
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, strings.TrimSpace(key.PublicKey))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// writeAuthorizedKeysFile installs content at /home/<username>/.ssh/authorized_keys
// with the ownership and modes sshd requires. The file is moved into place so
// readers never observe a partial write.
func (s *Service) writeAuthorizedKeysFile(ctx context.Context, username, content string) error {
	sshDir := HomeDir(username) + "/.ssh"
	keyPath := sshDir + "/authorized_keys"
	if err := s.runSudoOK(ctx, "mkdir", "-p", sshDir); err != nil {
		return fmt.Errorf("prepare ssh directory: %w", err)
	}
	if err := s.runSudoOK(ctx, "chown", username+":"+username, sshDir); err != nil {
		return fmt.Errorf("own ssh directory: %w", err)
	}
	if err := s.runSudoOK(ctx, "chmod", "700", sshDir); err != nil {
		return fmt.Errorf("mode ssh directory: %w", err)
	}
	if _, err := s.exec.RunSudoWithInput(ctx, content, "tee", keyPath+".new"); err != nil {
		return fmt.Errorf("write authorized_keys: %w", err)
	}
	if err := s.runSudoOK(ctx, "chown", username+":"+username, keyPath+".new"); err != nil {
		return fmt.Errorf("own authorized_keys: %w", err)
	}
	if err := s.runSudoOK(ctx, "chmod", "600", keyPath+".new"); err != nil {
		return fmt.Errorf("mode authorized_keys: %w", err)
	}
	if err := s.runSudoOK(ctx, "mv", "-f", keyPath+".new", keyPath); err != nil {
		return fmt.Errorf("install authorized_keys: %w", err)
	}
	return nil
}

// tempKeyFile writes a public key to a temporary file for ssh-keygen and
// returns a cleanup func.
func tempKeyFile(publicKey string) (path string, cleanup func(), err error) {
	tmp, err := os.CreateTemp("", "jenderal_pubkey_*.pub")
	if err != nil {
		return "", nil, err
	}
	name := tmp.Name()
	cleanup = func() { os.Remove(name) }
	if _, err := tmp.WriteString(strings.TrimSpace(publicKey) + "\n"); err != nil {
		tmp.Close()
		cleanup()
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return name, cleanup, nil
}
