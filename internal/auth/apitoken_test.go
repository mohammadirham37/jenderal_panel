package auth

import (
	"context"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestCreateAndValidateToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "tokenuser", "token@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	plainToken, apiToken, err := svc.CreateAPIToken(ctx, user.ID, "test-token")
	if err != nil {
		t.Fatalf("create api token: %v", err)
	}

	if plainToken == "" {
		t.Fatal("expected non-empty plain token")
	}
	if len(plainToken) != 64 {
		t.Errorf("expected 64-char hex token, got %d chars", len(plainToken))
	}
	if apiToken.ID == "" {
		t.Fatal("expected non-empty token ID")
	}
	if apiToken.Name != "test-token" {
		t.Errorf("expected name test-token, got %s", apiToken.Name)
	}

	validatedUser, err := svc.ValidateAPIToken(ctx, plainToken)
	if err != nil {
		t.Fatalf("validate api token: %v", err)
	}
	if validatedUser.ID != user.ID {
		t.Errorf("expected user ID %s, got %s", user.ID, validatedUser.ID)
	}
	if validatedUser.Username != "tokenuser" {
		t.Errorf("expected username tokenuser, got %s", validatedUser.Username)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	_, err := svc.ValidateAPIToken(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err != model.ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized for invalid token, got %v", err)
	}
}

func TestListTokens(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "tokenuser", "token@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, _, err = svc.CreateAPIToken(ctx, user.ID, "token-1")
	if err != nil {
		t.Fatalf("create token 1: %v", err)
	}
	_, _, err = svc.CreateAPIToken(ctx, user.ID, "token-2")
	if err != nil {
		t.Fatalf("create token 2: %v", err)
	}

	tokens, err := svc.ListAPITokens(ctx, user.ID)
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}

func TestDeleteToken(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "tokenuser", "token@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	plainToken, apiToken, err := svc.CreateAPIToken(ctx, user.ID, "deleteme")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	err = svc.DeleteAPIToken(ctx, apiToken.ID)
	if err != nil {
		t.Fatalf("delete token: %v", err)
	}

	_, err = svc.ValidateAPIToken(ctx, plainToken)
	if err != model.ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized after delete, got %v", err)
	}
}
