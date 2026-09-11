package sshaccount

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// keyTypeRegex accepts only bare OpenSSH public keys. Leading options
// (restrict, command="...", no-pty, ...) would change what sshd does when the
// key is used, so keys must start with the algorithm identifier.
var keyTypeRegex = regexp.MustCompile(`^(ssh-rsa|ssh-ed25519|ecdsa-sha2-nistp256|ecdsa-sha2-nistp384|ecdsa-sha2-nistp521) [A-Za-z0-9+/]+={0,2}( [^ \t\r\n]+)?$`)

// ListKeys returns the stored public keys for a user, oldest first.
func (s *Service) ListKeys(ctx context.Context, userID string) ([]model.SSHKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, public_key, fingerprint, algo, bits, created_at
		 FROM user_ssh_keys WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("list ssh keys: %w", err)
	}
	defer rows.Close()

	var keys []model.SSHKey
	for rows.Next() {
		var key model.SSHKey
		var createdStr string
		if err := rows.Scan(&key.ID, &key.UserID, &key.Name, &key.PublicKey,
			&key.Fingerprint, &key.Algo, &key.Bits, &createdStr); err != nil {
			return nil, fmt.Errorf("scan ssh key: %w", err)
		}
		key.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// AddKey validates a pasted OpenSSH public key with ssh-keygen, stores it, and
// rewrites authorized_keys from the database.
func (s *Service) AddKey(ctx context.Context, userID, name, publicKey string) (model.SSHKey, error) {
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return model.SSHKey{}, err
	}
	if !u.SSHEnabled {
		return model.SSHKey{}, model.NewValidationError("enable SSH access for this user before adding keys")
	}
	key, err := s.fingerprintKey(ctx, publicKey)
	if err != nil {
		return model.SSHKey{}, err
	}
	key.Name = sanitizeKeyName(name)
	if key.Name == "" {
		key.Name = key.Fingerprint
	}

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_ssh_keys WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return model.SSHKey{}, fmt.Errorf("count ssh keys: %w", err)
	}
	if count >= maxKeysPerUser {
		return model.SSHKey{}, model.NewValidationError("too many SSH keys; remove one first")
	}

	key.ID = ulid.Make().String()
	key.UserID = userID
	key.CreatedAt = time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO user_ssh_keys (id, user_id, name, public_key, fingerprint, algo, bits, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		key.ID, key.UserID, key.Name, key.PublicKey, key.Fingerprint, key.Algo, key.Bits,
		key.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return model.SSHKey{}, fmt.Errorf("insert ssh key: %w", err)
	}

	if err := s.RewriteAuthorizedKeys(ctx, userID); err != nil {
		return model.SSHKey{}, err
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_key_add", Module: "ssh", Target: u.Username, Detail: "added key " + key.Fingerprint})
	return key, nil
}

// DeleteKey removes a stored key and rewrites authorized_keys.
func (s *Service) DeleteKey(ctx context.Context, userID, keyID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM user_ssh_keys WHERE id = ? AND user_id = ?`, keyID, userID)
	if err != nil {
		return fmt.Errorf("delete ssh key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.ErrNotFound
	}
	if err := s.RewriteAuthorizedKeys(ctx, userID); err != nil {
		return err
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_key_delete", Module: "ssh", Target: keyID})
	return nil
}

// RewriteAuthorizedKeys regenerates /home/<username>/.ssh/authorized_keys from
// the database list. The database is the single source of truth, so removals
// take effect immediately and drift is impossible.
func (s *Service) RewriteAuthorizedKeys(ctx context.Context, userID string) error {
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
		// No Linux account to configure; nothing to converge.
		return nil
	}
	keys, err := s.ListKeys(ctx, userID)
	if err != nil {
		return err
	}
	return s.writeAuthorizedKeysFile(ctx, u.Username, authorizedKeysContent(keys))
}

// fingerprintKey parses the public key with the system ssh-keygen.
// The returned metadata comes only from ssh-keygen, never from client input.
func (s *Service) fingerprintKey(ctx context.Context, publicKey string) (model.SSHKey, error) {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return model.SSHKey{}, model.NewValidationError("public key is required")
	}
	if strings.ContainsAny(publicKey, "\r\n") || !keyTypeRegex.MatchString(publicKey) {
		return model.SSHKey{}, model.NewValidationError("key must be a single OpenSSH public key line (ssh-rsa, ssh-ed25519, or ecdsa) without options")
	}

	path, cleanup, err := tempKeyFile(publicKey)
	if err != nil {
		return model.SSHKey{}, fmt.Errorf("stage public key: %w", err)
	}
	defer cleanup()

	// ssh-keygen -lf prints: <bits> <fingerprint> <comment> (<TYPE>)
	result, err := s.exec.Run(ctx, "ssh-keygen", "-lf", path)
	if err != nil {
		return model.SSHKey{}, fmt.Errorf("validate public key: %w", err)
	}
	if result.ExitCode != 0 {
		return model.SSHKey{}, model.NewValidationError("invalid public key: " + firstLine(result.Stderr))
	}

	fields := strings.Fields(strings.TrimSpace(result.Stdout))
	if len(fields) < 3 {
		return model.SSHKey{}, model.NewValidationError("could not parse ssh-keygen output")
	}
	bits, err := strconv.Atoi(fields[0])
	if err != nil || bits <= 0 {
		return model.SSHKey{}, model.NewValidationError("could not parse key size")
	}
	algo := fields[len(fields)-1]
	algo = strings.TrimSuffix(strings.TrimPrefix(algo, "("), ")")
	if algo == "" {
		return model.SSHKey{}, model.NewValidationError("could not parse key algorithm")
	}
	if algo == "RSA" && bits < 2048 {
		return model.SSHKey{}, model.NewValidationError("RSA keys must be at least 2048 bits")
	}

	key := model.SSHKey{
		PublicKey:   publicKey,
		Fingerprint: fields[1],
		Algo:        algo,
		Bits:        bits,
	}
	return key, nil
}

// sanitizeKeyName strips control characters and caps the display name.
func sanitizeKeyName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		s = s[:idx]
	}
	return s
}
