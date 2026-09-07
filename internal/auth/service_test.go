package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testConfig() config.AuthConfig {
	return config.AuthConfig{
		SessionTTL: 24 * time.Hour,
		Argon2: config.Argon2Config{
			Memory:      64 * 1024,
			Iterations:  1,
			Parallelism: 1,
		},
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())

	hash, err := svc.HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if !svc.VerifyPassword(hash, "secret123") {
		t.Error("expected password to verify")
	}

	if svc.VerifyPassword(hash, "wrong") {
		t.Error("expected wrong password to not verify")
	}
}

func TestCreateAndAuthenticate(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if user.Username != "admin" {
		t.Errorf("expected username admin, got %s", user.Username)
	}
	if user.ID == "" {
		t.Error("expected non-empty ID")
	}
	if !user.IsActive {
		t.Error("expected user to be active")
	}

	authed, err := svc.Authenticate(ctx, "admin", "password123")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authed.ID != user.ID {
		t.Errorf("expected user ID %s, got %s", user.ID, authed.ID)
	}
}

func TestAuthenticateWrongPassword(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err = svc.Authenticate(ctx, "admin", "wrongpassword")
	if err != model.ErrInvalidCredentials {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthenticateInactiveUser(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "inactive", "inactive@test.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err = svc.UpdateUser(ctx, user.ID, "inactive", "inactive@test.com", false)
	if err != nil {
		t.Fatalf("update user: %v", err)
	}

	_, err = svc.Authenticate(ctx, "inactive", "password123")
	if err != model.ErrUserInactive {
		t.Errorf("expected ErrUserInactive, got %v", err)
	}
}

func TestDuplicateUser(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err = svc.CreateUser(ctx, "admin", "admin2@test.com", "password123")
	if err != model.ErrUserExists {
		t.Errorf("expected ErrUserExists, got %v", err)
	}
}

func TestGetUserByID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	found, err := svc.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user by id: %v", err)
	}
	if found.Username != "admin" {
		t.Errorf("expected username admin, got %s", found.Username)
	}

	_, err = svc.GetUserByID(ctx, "nonexistent")
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestListUsers(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	_, _ = svc.CreateUser(ctx, "user1", "user1@test.com", "pass")
	_, _ = svc.CreateUser(ctx, "user2", "user2@test.com", "pass")

	users, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestUpdatePassword(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "oldpass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	err = svc.UpdatePassword(ctx, user.ID, "newpass")
	if err != nil {
		t.Fatalf("update password: %v", err)
	}

	_, err = svc.Authenticate(ctx, "admin", "newpass")
	if err != nil {
		t.Errorf("expected authenticate with new password to succeed, got %v", err)
	}
}

func TestDeleteUser(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	err = svc.DeleteUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("delete user: %v", err)
	}

	_, err = svc.GetUserByID(ctx, user.ID)
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestCreateSessionAndGetAndDelete(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	session, err := svc.CreateSession(ctx, user.ID, "127.0.0.1", "TestBrowser/1.0")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if session.UserID != user.ID {
		t.Errorf("expected user ID %s, got %s", user.ID, session.UserID)
	}

	got, err := svc.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.ID != session.ID {
		t.Errorf("expected session ID %s, got %s", session.ID, got.ID)
	}

	err = svc.DeleteSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("delete session: %v", err)
	}

	_, err = svc.GetSession(ctx, session.ID)
	if err != model.ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestExpiredSession(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig()
	cfg.SessionTTL = -1 * time.Hour // already expired
	svc := NewService(db, cfg)
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	session, err := svc.CreateSession(ctx, user.ID, "127.0.0.1", "TestBrowser/1.0")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err = svc.GetSession(ctx, session.ID)
	if err != model.ErrSessionExpired {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}
}

func TestCleanExpiredSessions(t *testing.T) {
	db := setupTestDB(t)
	cfg := testConfig()
	cfg.SessionTTL = -1 * time.Hour
	svc := NewService(db, cfg)
	ctx := context.Background()

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err = svc.CreateSession(ctx, user.ID, "127.0.0.1", "TestBrowser/1.0")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	err = svc.CleanExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("clean expired sessions: %v", err)
	}
}
