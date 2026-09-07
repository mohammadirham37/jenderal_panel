package auth

import (
	"context"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func TestTOTPSetup(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "totpuser", "totp@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	secret, url, err := svc.TOTPSetup(ctx, user.ID)
	if err != nil {
		t.Fatalf("totp setup: %v", err)
	}

	if secret == "" {
		t.Error("expected non-empty secret")
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}

	// TOTP should not be enabled yet.
	enabled, err := svc.IsTOTPEnabled(ctx, user.ID)
	if err != nil {
		t.Fatalf("is totp enabled: %v", err)
	}
	if enabled {
		t.Error("expected TOTP to be disabled after setup")
	}
}

func TestTOTPEnableAndVerify(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "totpuser", "totp@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	secret, _, err := svc.TOTPSetup(ctx, user.ID)
	if err != nil {
		t.Fatalf("totp setup: %v", err)
	}

	// Generate a valid code from the secret.
	code, err := totp.GenerateCodeCustom(secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:     1,
		Digits:   otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}

	// Enable with valid code.
	err = svc.TOTPEnable(ctx, user.ID, code)
	if err != nil {
		t.Fatalf("totp enable: %v", err)
	}

	// TOTP should now be enabled.
	enabled, err := svc.IsTOTPEnabled(ctx, user.ID)
	if err != nil {
		t.Fatalf("is totp enabled: %v", err)
	}
	if !enabled {
		t.Error("expected TOTP to be enabled")
	}

	// Verify with a freshly generated code.
	code2, err := totp.GenerateCodeCustom(secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:     1,
		Digits:   otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}

	valid, err := svc.TOTPVerify(ctx, user.ID, code2)
	if err != nil {
		t.Fatalf("totp verify: %v", err)
	}
	if !valid {
		t.Error("expected valid TOTP code to verify")
	}

	// Invalid code should fail.
	valid, err = svc.TOTPVerify(ctx, user.ID, "000000")
	if err != nil {
		t.Fatalf("totp verify invalid: %v", err)
	}
	if valid {
		t.Error("expected invalid TOTP code to fail")
	}
}

func TestTOTPDisable(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "totpuser", "totp@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, _, err = svc.TOTPSetup(ctx, user.ID)
	if err != nil {
		t.Fatalf("totp setup: %v", err)
	}

	err = svc.TOTPDisable(ctx, user.ID)
	if err != nil {
		t.Fatalf("totp disable: %v", err)
	}

	enabled, err := svc.IsTOTPEnabled(ctx, user.ID)
	if err != nil {
		t.Fatalf("is totp enabled: %v", err)
	}
	if enabled {
		t.Error("expected TOTP to be disabled after disable")
	}
}

func TestIsTOTPEnabled_NoRecord(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "notp", "notp@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	enabled, err := svc.IsTOTPEnabled(ctx, user.ID)
	if err != nil {
		t.Fatalf("is totp enabled: %v", err)
	}
	if enabled {
		t.Error("expected TOTP to be disabled for user without setup")
	}
}
