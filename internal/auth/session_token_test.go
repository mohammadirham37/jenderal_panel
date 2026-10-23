package auth

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSessionTokensAreRandomAndStoredHashed(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()
	user, err := svc.CreateUser(ctx, "alice", "alice@test.com", "pass")
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	var token string
	for i := 0; i < 20; i++ {
		session, err := svc.CreateSession(ctx, user.ID, "127.0.0.1", "test")
		if err != nil {
			t.Fatal(err)
		}
		token = session.ID
		if len(token) < 40 {
			t.Fatalf("session token %q is too short to resist guessing", token)
		}
		if seen[token] {
			t.Fatal("session tokens repeat")
		}
		seen[token] = true

		var stored string
		if err := db.QueryRow(`SELECT id FROM sessions WHERE user_id = ? ORDER BY created_at DESC LIMIT 1`, user.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored == token {
			t.Fatal("the raw session token is stored in the database")
		}
		if _, err := hex.DecodeString(stored); err != nil || len(stored) != 64 {
			t.Fatalf("stored session id %q is not a sha256 hash", stored)
		}
	}

	// The token still resolves the session, and the raw value is never
	// returned by a lookup.
	session, err := svc.GetSession(ctx, token)
	if err != nil || session.UserID != user.ID {
		t.Fatalf("GetSession(token) = %+v, %v", session, err)
	}
	if _, err := svc.GetSession(ctx, hashSessionToken(token)); err == nil {
		t.Error("the stored hash must not be usable as a session token")
	}
	if err := svc.DeleteSession(ctx, token); err != nil {
		t.Fatalf("DeleteSession(token): %v", err)
	}
	if _, err := svc.GetSession(ctx, token); err == nil {
		t.Error("session survived deletion")
	}
}

func TestListedSessionsAreRevocableButNotUsable(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()
	user, err := svc.CreateUser(ctx, "bob", "bob@test.com", "pass")
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateSession(ctx, user.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := svc.ListSessions(ctx, user.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions = %v, %v", sessions, err)
	}
	listed := sessions[0].ID
	if listed == created.ID {
		t.Fatal("listing sessions exposes the usable session token")
	}
	if _, err := svc.GetSessionForUser(ctx, listed, user.ID); err != nil {
		t.Fatalf("the listed id must stay usable for revocation: %v", err)
	}
	if err := svc.DeleteSessionByStoredID(ctx, listed); err != nil {
		t.Fatalf("DeleteSessionByStoredID: %v", err)
	}
	if _, err := svc.GetSession(ctx, created.ID); err == nil {
		t.Error("revoking the listed session did not end it")
	}
}

func TestImpersonationUsesStoredAdminSessionID(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, testConfig())
	ctx := context.Background()
	admin, err := svc.CreateUser(ctx, "boss", "boss@test.com", "pass")
	if err != nil {
		t.Fatal(err)
	}
	target, err := svc.CreateUser(ctx, "tenant", "tenant@test.com", "pass")
	if err != nil {
		t.Fatal(err)
	}
	adminSession, err := svc.CreateSession(ctx, admin.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	imp, err := svc.CreateImpersonationSession(ctx, target.ID, adminSession.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.GetSession(ctx, imp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ImpersonatorSessionID == adminSession.ID {
		t.Fatal("the admin's session token is reachable from the impersonated session")
	}
	info, err := svc.ImpersonatedBy(ctx, loaded.ImpersonatorSessionID)
	if err != nil || info.AdminUserID != admin.ID {
		t.Fatalf("ImpersonatedBy = %+v, %v", info, err)
	}
	if strings.TrimSpace(loaded.ImpersonatorSessionID) == "" {
		t.Fatal("the impersonator reference was lost")
	}
}
