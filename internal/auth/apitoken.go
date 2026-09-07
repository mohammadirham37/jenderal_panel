package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// CreateAPIToken generates a new API token for the given user. The plain-text
// token is returned exactly once; only its SHA-256 hash is persisted.
func (s *Service) CreateAPIToken(ctx context.Context, userID, name string) (token string, apiToken model.APIToken, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", model.APIToken{}, fmt.Errorf("generate token: %w", err)
	}

	plainToken := hex.EncodeToString(raw) // 64 hex chars
	hash := sha256.Sum256([]byte(plainToken))
	tokenHash := hex.EncodeToString(hash[:])

	now := time.Now().UTC()
	id := ulid.Make().String()

	apiToken = model.APIToken{
		ID:        id,
		UserID:    userID,
		Name:      name,
		TokenHash: tokenHash,
		ExpiresAt: now.Add(365 * 24 * time.Hour), // 1 year default
		CreatedAt: now,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (id, user_id, name, token_hash, last_used, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		apiToken.ID, apiToken.UserID, apiToken.Name, apiToken.TokenHash,
		"",
		apiToken.ExpiresAt.Format(time.RFC3339),
		apiToken.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return "", model.APIToken{}, fmt.Errorf("insert api token: %w", err)
	}

	return plainToken, apiToken, nil
}

// ListAPITokens returns all API tokens belonging to the given user.
func (s *Service) ListAPITokens(ctx context.Context, userID string) ([]model.APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, token_hash, last_used, expires_at, created_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query api tokens: %w", err)
	}
	defer rows.Close()

	var tokens []model.APIToken
	for rows.Next() {
		var t model.APIToken
		var lastUsedStr, expiresStr, createdStr string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &lastUsedStr, &expiresStr, &createdStr); err != nil {
			return nil, fmt.Errorf("scan api token: %w", err)
		}
		t.LastUsed, _ = time.Parse(time.RFC3339, lastUsedStr)
		t.ExpiresAt, _ = time.Parse(time.RFC3339, expiresStr)
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		tokens = append(tokens, t)
	}

	return tokens, rows.Err()
}

// DeleteAPIToken removes an API token by its ID.
func (s *Service) DeleteAPIToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete api token: %w", err)
	}
	return nil
}

// ValidateAPIToken hashes the incoming plain-text token, looks it up by hash,
// checks expiry, updates last_used, and returns the owning user.
func (s *Service) ValidateAPIToken(ctx context.Context, token string) (model.User, error) {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	var userID string
	var expiresStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at FROM api_tokens WHERE token_hash = ?`, tokenHash,
	).Scan(&userID, &expiresStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.User{}, model.ErrUnauthorized
		}
		return model.User{}, fmt.Errorf("query api token: %w", err)
	}

	expiresAt, _ := time.Parse(time.RFC3339, expiresStr)
	if !expiresAt.IsZero() && time.Now().UTC().After(expiresAt) {
		return model.User{}, model.ErrUnauthorized
	}

	// Update last_used timestamp.
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used = ? WHERE token_hash = ?`, now, tokenHash,
	)

	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return model.User{}, fmt.Errorf("get token user: %w", err)
	}

	if !user.IsActive {
		return model.User{}, model.ErrUserInactive
	}

	return user, nil
}
