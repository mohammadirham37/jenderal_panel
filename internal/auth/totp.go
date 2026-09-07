package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTPSetup generates a new TOTP secret for the user and stores it (disabled).
// Returns the base32-encoded secret and the otpauth:// provisioning URL.
func (s *Service) TOTPSetup(ctx context.Context, userID string) (secret string, url string, err error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", fmt.Errorf("get user for totp setup: %w", err)
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Jenderal Panel",
		AccountName: user.Username,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate totp key: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_totp (user_id, secret, enabled, created_at) VALUES (?, ?, 0, ?)`,
		userID, key.Secret(), now,
	)
	if err != nil {
		return "", "", fmt.Errorf("store totp secret: %w", err)
	}

	return key.Secret(), key.URL(), nil
}

// TOTPEnable verifies the provided code and enables TOTP for the user.
func (s *Service) TOTPEnable(ctx context.Context, userID, code string) error {
	secret, err := s.getTOTPSecret(ctx, userID)
	if err != nil {
		return err
	}

	valid := totp.Validate(code, secret)
	if !valid {
		return fmt.Errorf("invalid totp code")
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE user_totp SET enabled = 1 WHERE user_id = ?`, userID,
	)
	if err != nil {
		return fmt.Errorf("enable totp: %w", err)
	}

	return nil
}

// TOTPDisable removes TOTP configuration for the user.
func (s *Service) TOTPDisable(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_totp WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("disable totp: %w", err)
	}
	return nil
}

// TOTPVerify validates a TOTP code for the user.
func (s *Service) TOTPVerify(ctx context.Context, userID, code string) (bool, error) {
	secret, err := s.getTOTPSecret(ctx, userID)
	if err != nil {
		return false, err
	}

	valid, err := totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:     1,
		Digits:   otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false, fmt.Errorf("validate totp: %w", err)
	}

	return valid, nil
}

// IsTOTPEnabled checks whether TOTP is enabled for the user.
func (s *Service) IsTOTPEnabled(ctx context.Context, userID string) (bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx,
		`SELECT enabled FROM user_totp WHERE user_id = ?`, userID,
	).Scan(&enabled)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("check totp enabled: %w", err)
	}

	return enabled == 1, nil
}

// getTOTPSecret retrieves the stored TOTP secret for a user.
func (s *Service) getTOTPSecret(ctx context.Context, userID string) (string, error) {
	var secret string
	err := s.db.QueryRowContext(ctx,
		`SELECT secret FROM user_totp WHERE user_id = ?`, userID,
	).Scan(&secret)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("totp not configured for user")
		}
		return "", fmt.Errorf("get totp secret: %w", err)
	}
	return secret, nil
}
