// Package sshaccount provisions panel users as Linux SSH accounts and keeps
// their access to the websites they own in sync. Accounts are created with a
// locked password and the authorized_keys file is always rewritten from the
// panel database. SetPassword mirrors the panel password onto the Linux
// account so SSH password authentication uses the same credentials.
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

// SetPassword mirrors a panel password onto the Linux account so SSH password
// authentication accepts the same credentials as the panel. It is a no-op for
// users without SSH access or without a provisioned Linux account. The
// password travels to chpasswd over stdin so it never appears in a process
// listing.
func (s *Service) SetPassword(ctx context.Context, userID, password string) error {
	if password == "" {
		return model.NewValidationError("password is required")
	}
	if strings.ContainsAny(password, "\n\r") {
		return model.NewValidationError("password must not contain line breaks")
	}
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return err
	}
	if !u.SSHEnabled {
		return nil
	}
	if !ValidateUsername(u.Username) {
		return ErrInvalidUsername
	}
	exists, err := s.accountExists(ctx, u.Username)
	if err != nil {
		return err
	}
	if !exists {
		// Without a Linux account there is nothing to sync; provisioning
		// callers pass the password to SetPassword right after Provision.
		return nil
	}
	result, err := s.exec.RunSudoWithInput(ctx, u.Username+":"+password+"\n", "chpasswd")
	if err != nil {
		return fmt.Errorf("sync account password: %w", err)
	}
	if result == nil || result.ExitCode != 0 {
		detail := "executor returned no result"
		if result != nil {
			detail = strings.TrimSpace(result.Stderr)
			if detail == "" {
				detail = fmt.Sprintf("exit status %d", result.ExitCode)
			}
		}
		return fmt.Errorf("sync account password %s: %s", u.Username, detail)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_account_password_sync", Module: "ssh", Target: u.Username, Detail: "synced SSH password for " + u.Username})
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

// ReconcileAll re-syncs website access for every panel user with SSH enabled.
// It runs at panel startup so sites provisioned after an account existed — or
// grants lost on older installs — converge without manual action.
func (s *Service) ReconcileAll(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM users WHERE ssh_enabled = 1`)
	if err != nil {
		return fmt.Errorf("list ssh-enabled users: %w", err)
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan ssh-enabled users: %w", err)
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read ssh-enabled users: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close ssh-enabled users: %w", err)
	}

	var failures []error
	for _, id := range userIDs {
		if err := s.SyncOwnedWebsites(ctx, id); err != nil {
			failures = append(failures, fmt.Errorf("user %s: %w", id, err))
		}
	}
	return errors.Join(failures...)
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

// GrantWebsiteOwner grants the owning panel user's Linux account access to a
// single website (group membership plus ACLs). Safe to run repeatedly; it is
// a no-op when the site has no owner, the owner has no SSH account, or the
// Linux account is missing.
func (s *Service) GrantWebsiteOwner(ctx context.Context, websiteID string) error {
	var ownerID, webUser string
	err := s.db.QueryRowContext(ctx,
		`SELECT created_by, web_user FROM websites WHERE id = ?`, websiteID,
	).Scan(&ownerID, &webUser)
	if err == sql.ErrNoRows {
		return model.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load website: %w", err)
	}
	if ownerID == "" {
		return model.NewValidationError("website has no owning panel user")
	}
	if !webUserRegex.MatchString(webUser) {
		return model.NewValidationError("unsafe website user name")
	}
	u, err := s.loadUser(ctx, ownerID)
	if err != nil {
		return err
	}
	if !u.SSHEnabled {
		return nil
	}
	if !ValidateUsername(u.Username) {
		return nil
	}
	exists, err := s.accountExists(ctx, u.Username)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := s.GrantWebsite(ctx, u.Username, webUser); err != nil {
		return err
	}
	// Verify the account can actually enter the site home now. When only the
	// group-bit fallback applied, membership starts on the next login — the
	// caller must know the repair did not take effect yet.
	siteHome := "/home/" + webUser
	if !s.accountCanEnter(ctx, u.Username, siteHome) {
		return model.NewDomainError("SSH_ACCESS_NOT_EFFECTIVE",
			"access was applied but "+u.Username+" cannot enter "+siteHome+
				" yet; log out and log back in (or reconnect SSH), then try again", nil)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{
		Action: "ssh_account_website_grant",
		Module: "ssh",
		Target: websiteID,
		Detail: "granted " + u.Username + " access to " + webUser,
	})
	return nil
}

// accountCanEnter reports whether username can traverse dir right now, as
// that account.
func (s *Service) accountCanEnter(ctx context.Context, username, dir string) bool {
	result, err := s.exec.RunSudo(ctx, "-u", username, "--", "/usr/bin/test", "-x", dir)
	if err != nil || result == nil {
		return false
	}
	return result.ExitCode == 0
}

// GrantWebsite gives a panel account group membership plus POSIX ACL
// read/write access to a website's project directory. ACLs apply to current
// sessions immediately; when the acl tooling is missing it is installed
// first, with group-write permissions as the last resort (that fallback only
// takes effect on the account's next login).
func (s *Service) GrantWebsite(ctx context.Context, username, webUser string) error {
	if !ValidateUsername(username) || !webUserRegex.MatchString(webUser) {
		return model.NewValidationError("unsafe account or website user name")
	}
	if err := s.runSudoOK(ctx, "usermod", "-aG", webUser, username); err != nil {
		return fmt.Errorf("join website group: %w", err)
	}
	siteHome := "/home/" + webUser
	if !s.setfaclAvailable(ctx) {
		if _, err := s.exec.RunSudo(ctx, "apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120", "acl"); err != nil {
			return fmt.Errorf("install acl package: %w", err)
		}
	}
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
