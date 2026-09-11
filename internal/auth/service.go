package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/argon2"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Service struct {
	db  *sql.DB
	cfg config.AuthConfig
}

func NewService(db *sql.DB, cfg config.AuthConfig) *Service {
	return &Service{db: db, cfg: cfg}
}

// HashPassword hashes a password using argon2id with config params.
// Format: $argon2id$v=19$m=X,t=X,p=X$SALT_HEX$HASH_HEX
func (s *Service) HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		s.cfg.Argon2.Iterations,
		s.cfg.Argon2.Memory,
		s.cfg.Argon2.Parallelism,
		32,
	)

	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		s.cfg.Argon2.Memory,
		s.cfg.Argon2.Iterations,
		s.cfg.Argon2.Parallelism,
		hex.EncodeToString(salt),
		hex.EncodeToString(hash),
	)

	return encoded, nil
}

// VerifyPassword parses the encoded string and verifies the password.
func (s *Service) VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	// Expected: ["", "argon2id", "v=19", "m=X,t=X,p=X", "SALT_HEX", "HASH_HEX"]
	if len(parts) != 6 {
		return false
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return false
	}

	salt, err := hex.DecodeString(parts[4])
	if err != nil {
		return false
	}

	expectedHash, err := hex.DecodeString(parts[5])
	if err != nil {
		return false
	}

	hash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expectedHash)))

	return subtle.ConstantTimeCompare(hash, expectedHash) == 1
}

// CreateUser creates a new user with hashed password.
func (s *Service) CreateUser(ctx context.Context, username, email, password string) (model.User, error) {
	hashed, err := s.HashPassword(password)
	if err != nil {
		return model.User{}, fmt.Errorf("hash password: %w", err)
	}

	now := time.Now().UTC()
	id := ulid.Make().String()

	user := model.User{
		ID:        id,
		Username:  username,
		Email:     email,
		Password:  hashed,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password, is_active, ssh_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.Email, user.Password,
		boolToInt(user.IsActive), boolToInt(user.SSHEnabled),
		user.CreatedAt.Format(time.RFC3339),
		user.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return model.User{}, model.ErrUserExists
		}
		return model.User{}, fmt.Errorf("insert user: %w", err)
	}

	return user, nil
}

// Authenticate checks credentials and returns the user.
func (s *Service) Authenticate(ctx context.Context, username, password string) (model.User, error) {
	var user model.User
	var isActiveInt, sshEnabled int
	var createdStr, updatedStr string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, is_active, ssh_enabled, created_at, updated_at
		 FROM users WHERE username = ?`, username,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Password, &isActiveInt, &sshEnabled, &createdStr, &updatedStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.User{}, model.ErrInvalidCredentials
		}
		return model.User{}, fmt.Errorf("query user: %w", err)
	}

	user.IsActive = isActiveInt == 1
	user.SSHEnabled = sshEnabled == 1
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	if !user.IsActive {
		return model.User{}, model.ErrUserInactive
	}

	if !s.VerifyPassword(user.Password, password) {
		return model.User{}, model.ErrInvalidCredentials
	}

	return user, nil
}

// GetUserByID returns a user by their ID.
func (s *Service) GetUserByID(ctx context.Context, id string) (model.User, error) {
	var user model.User
	var isActiveInt, sshEnabled int
	var createdStr, updatedStr string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, password, is_active, ssh_enabled, created_at, updated_at
		 FROM users WHERE id = ?`, id,
	).Scan(&user.ID, &user.Username, &user.Email, &user.Password, &isActiveInt, &sshEnabled, &createdStr, &updatedStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.User{}, model.ErrNotFound
		}
		return model.User{}, fmt.Errorf("query user: %w", err)
	}

	user.IsActive = isActiveInt == 1
	user.SSHEnabled = sshEnabled == 1
	user.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	user.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return user, nil
}

// ListUsers returns all users ordered by created_at DESC.
func (s *Service) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, email, password, is_active, ssh_enabled, created_at, updated_at
		 FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		var isActiveInt, sshEnabled int
		var createdStr, updatedStr string
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Password, &isActiveInt, &sshEnabled, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.IsActive = isActiveInt == 1
		u.SSHEnabled = sshEnabled == 1
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		users = append(users, u)
	}

	return users, rows.Err()
}

// UpdateUser updates a user's username, email, and active status.
func (s *Service) UpdateUser(ctx context.Context, id, username, email string, isActive bool) (model.User, error) {
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET username = ?, email = ?, is_active = ?, updated_at = ? WHERE id = ?`,
		username, email, boolToInt(isActive), now.Format(time.RFC3339), id,
	)
	if err != nil {
		return model.User{}, fmt.Errorf("update user: %w", err)
	}

	return s.GetUserByID(ctx, id)
}

// SetSSHEnabled records whether the panel user also owns a Linux SSH
// account. The account itself is provisioned by the sshaccount module.
func (s *Service) SetSSHEnabled(ctx context.Context, id string, enabled bool) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET ssh_enabled = ?, updated_at = ? WHERE id = ?`,
		boolToInt(enabled), now.Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("update ssh enabled: %w", err)
	}
	return nil
}

// UpdatePassword updates a user's password.
func (s *Service) UpdatePassword(ctx context.Context, id, password string) error {
	hashed, err := s.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET password = ?, updated_at = ? WHERE id = ?`,
		hashed, now.Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	return nil
}

// DeleteUser deletes a user by ID.
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// CreateSession creates a new session for a user.
func (s *Service) CreateSession(ctx context.Context, userID, ip, userAgent string) (model.Session, error) {
	now := time.Now().UTC()
	id := ulid.Make().String()

	session := model.Session{
		ID:        id,
		UserID:    userID,
		IPAddress: ip,
		UserAgent: userAgent,
		ExpiresAt: now.Add(s.cfg.SessionTTL),
		CreatedAt: now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, ip_address, user_agent, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.IPAddress, session.UserAgent,
		session.ExpiresAt.Format(time.RFC3339),
		session.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return model.Session{}, fmt.Errorf("insert session: %w", err)
	}

	return session, nil
}

// GetSession returns a session by ID. Deletes and returns ErrSessionExpired if expired.
func (s *Service) GetSession(ctx context.Context, id string) (model.Session, error) {
	var session model.Session
	var expiresStr, createdStr string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, ip_address, user_agent, expires_at, created_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(&session.ID, &session.UserID, &session.IPAddress, &session.UserAgent, &expiresStr, &createdStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.Session{}, model.ErrNotFound
		}
		return model.Session{}, fmt.Errorf("query session: %w", err)
	}

	session.ExpiresAt, _ = time.Parse(time.RFC3339, expiresStr)
	session.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.DeleteSession(ctx, id)
		return model.Session{}, model.ErrSessionExpired
	}

	return session, nil
}

// DeleteSession deletes a session by ID.
func (s *Service) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// CleanExpiredSessions deletes all expired sessions.
func (s *Service) CleanExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("clean expired sessions: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
