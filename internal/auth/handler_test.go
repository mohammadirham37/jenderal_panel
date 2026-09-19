package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// The SPA sidebar gates admin-only items (e.g. the terminal) on the roles
// store, which is only populated from the login response or /auth/me. If the
// login response omits roles, the terminal menu stays hidden until the next
// full page reload.
func TestLoginResponseIncludesRoles(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	svc := NewService(db, testConfig())
	rbac := NewRBAC(db)
	if err := rbac.Seed(ctx); err != nil {
		t.Fatalf("seed rbac: %v", err)
	}
	h := NewHandler(svc, rbac, audit.NewService(db))

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := rbac.AssignRole(ctx, user.ID, "admin"); err != nil {
		t.Fatalf("assign role: %v", err)
	}

	body := bytes.NewBufferString(`{"username":"admin","password":"password123"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			User  model.User   `json:"user"`
			Roles []model.Role `json:"roles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	hasAdmin := false
	for _, r := range resp.Data.Roles {
		if r.Name == "admin" {
			hasAdmin = true
		}
	}
	if !hasAdmin {
		t.Errorf("expected login response roles to contain admin, got %+v", resp.Data.Roles)
	}
}

func sessionHandlerRequest(t *testing.T, method, path, sessionID string, user model.User, session model.Session) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rctx := chi.NewRouteContext()
	if sessionID != "" {
		rctx.URLParams.Add("sessionID", sessionID)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, userContextKey, user)
	ctx = context.WithValue(ctx, sessionContextKey, session)
	return req.WithContext(ctx)
}

// Revoking must be limited to the caller's own sessions.
func TestRevokeSessionRejectsForeignSession(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	svc := NewService(db, testConfig())
	h := NewHandler(svc, NewRBAC(db), audit.NewService(db))

	owner, err := svc.CreateUser(ctx, "owner", "owner@test.com", "pass")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	attacker, err := svc.CreateUser(ctx, "attacker", "attacker@test.com", "pass")
	if err != nil {
		t.Fatalf("create attacker: %v", err)
	}
	ownerSession, err := svc.CreateSession(ctx, owner.ID, "1.2.3.4", "Browser/1.0")
	if err != nil {
		t.Fatalf("create owner session: %v", err)
	}
	attackerSession, err := svc.CreateSession(ctx, attacker.ID, "5.6.7.8", "Browser/1.0")
	if err != nil {
		t.Fatalf("create attacker session: %v", err)
	}

	req := sessionHandlerRequest(t, http.MethodDelete, "/api/v1/auth/sessions/"+ownerSession.ID, ownerSession.ID, attacker, attackerSession)
	rec := httptest.NewRecorder()
	h.RevokeSession(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := svc.GetSession(ctx, ownerSession.ID); err != nil {
		t.Errorf("expected foreign session to survive, got %v", err)
	}
}

// The owner can revoke their own session, and listing marks the current one.
func TestRevokeSessionAndListMarksCurrent(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	svc := NewService(db, testConfig())
	h := NewHandler(svc, NewRBAC(db), audit.NewService(db))

	user, err := svc.CreateUser(ctx, "admin", "admin@test.com", "pass")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	currentSession, err := svc.CreateSession(ctx, user.ID, "1.2.3.4", "CurrentBrowser/1.0")
	if err != nil {
		t.Fatalf("create current session: %v", err)
	}
	otherSession, err := svc.CreateSession(ctx, user.ID, "5.6.7.8", "OtherBrowser/1.0")
	if err != nil {
		t.Fatalf("create other session: %v", err)
	}

	req := sessionHandlerRequest(t, http.MethodGet, "/api/v1/auth/sessions", "", user, currentSession)
	rec := httptest.NewRecorder()
	h.ListSessions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.Data))
	}
	currentMarked := 0
	for _, s := range resp.Data {
		if s.Current {
			currentMarked++
			if s.ID != currentSession.ID {
				t.Errorf("current flag on wrong session %s", s.ID)
			}
		}
	}
	if currentMarked != 1 {
		t.Errorf("expected exactly 1 current session, got %d", currentMarked)
	}

	// Revoke the other session.
	req = sessionHandlerRequest(t, http.MethodDelete, "/api/v1/auth/sessions/"+otherSession.ID, otherSession.ID, user, currentSession)
	rec = httptest.NewRecorder()
	h.RevokeSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on revoke, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := svc.GetSession(ctx, otherSession.ID); err != model.ErrNotFound {
		t.Errorf("expected revoked session to be gone, got %v", err)
	}
	if _, err := svc.GetSession(ctx, currentSession.ID); err != nil {
		t.Errorf("expected current session to survive, got %v", err)
	}
}
