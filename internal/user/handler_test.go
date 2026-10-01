package user

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/model"

	_ "github.com/mattn/go-sqlite3"
)

// The password length check runs before any database access, so a nil DB is
// fine for these tests.
func newPolicyTestHandler() *Handler {
	cfg := config.AuthConfig{
		Argon2: config.Argon2Config{Memory: 64 * 1024, Iterations: 1, Parallelism: 1},
	}
	return NewHandler(auth.NewService(nil, cfg), auth.NewRBAC(nil), audit.NewService(nil), nil)
}

func createWithPassword(t *testing.T, h *Handler, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := bytes.NewBufferString(`{"username":"tester","email":"t@example.com","password":` + quote(password) + `,"role":"user"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	return rec
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestCreateRejectsShortPassword(t *testing.T) {
	h := newPolicyTestHandler()
	if rec := createWithPassword(t, h, "short"); rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a %d-character password, got %d", len("short"), rec.Code)
	}
}

func TestValidatePasswordLength(t *testing.T) {
	if err := validatePasswordLength("short"); err == nil {
		t.Error("expected a 5-character password to be rejected")
	}
	if err := validatePasswordLength("12345678"); err != nil {
		t.Errorf("expected an 8-character password to pass, got %v", err)
	}
	if err := validatePasswordLength("long enough with spaces 1"); err != nil {
		t.Errorf("expected a long passphrase to pass, got %v", err)
	}
}

// loginAsTestDB builds a migrated in-memory database plus the handler under
// test, mirroring the harness in the auth package.
func loginAsTestDB(t *testing.T) (*Handler, *auth.Service, *auth.RBAC, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := config.AuthConfig{
		SessionTTL: time.Hour,
		Argon2:     config.Argon2Config{Memory: 64 * 1024, Iterations: 1, Parallelism: 1},
	}
	svc := auth.NewService(db, cfg)
	rbac := auth.NewRBAC(db)
	if err := rbac.Seed(context.Background()); err != nil {
		t.Fatalf("seed rbac: %v", err)
	}
	return NewHandler(svc, rbac, audit.NewService(db), nil), svc, rbac, db
}

// loginAsRequest injects the caller and their session the way the auth
// middleware would.
func loginAsRequest(t *testing.T, h *Handler, targetID string, caller model.User, session model.Session) *httptest.ResponseRecorder {
	t.Helper()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", targetID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID+"/login-as", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	ctx := auth.WithUserContext(req.Context(), caller)
	ctx = auth.WithSessionContext(ctx, session)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	h.LoginAs(rec, req)
	return rec
}

func TestLoginAsCreatesImpersonationSession(t *testing.T) {
	h, svc, rbac, db := loginAsTestDB(t)
	ctx := context.Background()

	admin, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := rbac.AssignRole(ctx, admin.ID, "admin"); err != nil {
		t.Fatalf("assign admin role: %v", err)
	}
	target, err := svc.CreateUser(ctx, "customer", "customer@test.com", "password123")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	adminSession, err := svc.CreateSession(ctx, admin.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	rec := loginAsRequest(t, h, target.ID, admin, adminSession)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			User           model.User            `json:"user"`
			Impersonation  model.ImpersonationInfo `json:"impersonation"`
			CSRFToken      string                `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.User.ID != target.ID {
		t.Errorf("expected target user %s, got %s", target.ID, resp.Data.User.ID)
	}
	if resp.Data.Impersonation.AdminUserID != admin.ID {
		t.Errorf("expected impersonation admin %s, got %s", admin.ID, resp.Data.Impersonation.AdminUserID)
	}
	if resp.Data.CSRFToken == "" {
		t.Error("expected a CSRF token in the response")
	}

	// The new cookie must be the impersonation session, not the admin's.
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session_id" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || sessionCookie.Value == adminSession.ID {
		t.Fatalf("expected a fresh session cookie, got %+v", sessionCookie)
	}
	impSession, err := svc.GetSession(ctx, sessionCookie.Value)
	if err != nil {
		t.Fatalf("impersonation session missing: %v", err)
	}
	if impSession.UserID != target.ID {
		t.Errorf("expected session for %s, got %s", target.ID, impSession.UserID)
	}
	if impSession.ImpersonatorSessionID != adminSession.ID {
		t.Errorf("expected impersonator link %s, got %q", adminSession.ID, impSession.ImpersonatorSessionID)
	}

	// /auth/me through the impersonation session must expose the admin.
	me := auth.NewHandler(svc, rbac, audit.NewService(db))
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq = meReq.WithContext(auth.WithSessionContext(auth.WithUserContext(meReq.Context(), target), impSession))
	meRec := httptest.NewRecorder()
	me.Me(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("expected /auth/me 200, got %d", meRec.Code)
	}
	var meResp struct {
		Data struct {
			Impersonation *model.ImpersonationInfo `json:"impersonation"`
		} `json:"data"`
	}
	if err := json.Unmarshal(meRec.Body.Bytes(), &meResp); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if meResp.Data.Impersonation == nil || meResp.Data.Impersonation.AdminUserID != admin.ID {
		t.Errorf("expected /auth/me to report impersonation by %s, got %+v", admin.ID, meResp.Data.Impersonation)
	}
}

func TestStopImpersonationRestoresAdminSession(t *testing.T) {
	_, svc, rbac, db := loginAsTestDB(t)
	authH := auth.NewHandler(svc, rbac, audit.NewService(db))
	ctx := context.Background()

	admin, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	target, err := svc.CreateUser(ctx, "customer", "customer@test.com", "password123")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	adminSession, err := svc.CreateSession(ctx, admin.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	impSession, err := svc.CreateImpersonationSession(ctx, target.ID, adminSession.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("create impersonation session: %v", err)
	}

	stopReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/impersonate/stop", nil)
	stopReq = stopReq.WithContext(auth.WithSessionContext(auth.WithUserContext(stopReq.Context(), target), impSession))
	stopRec := httptest.NewRecorder()
	authH.StopImpersonation(stopRec, stopReq)
	if stopRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", stopRec.Code, stopRec.Body.String())
	}

	var resp struct {
		Data struct {
			User      model.User `json:"user"`
			CSRFToken string     `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stopRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.User.ID != admin.ID {
		t.Errorf("expected the admin user back, got %s", resp.Data.User.ID)
	}

	// The impersonation session must be gone; the admin session must remain.
	if _, err := svc.GetSession(ctx, impSession.ID); err == nil {
		t.Error("expected the impersonation session to be deleted")
	}
	if _, err := svc.GetSession(ctx, adminSession.ID); err != nil {
		t.Errorf("expected the admin session to survive: %v", err)
	}
}

func TestLoginAsRejectsSelfAndInactive(t *testing.T) {
	h, svc, _, _ := loginAsTestDB(t)
	ctx := context.Background()

	admin, err := svc.CreateUser(ctx, "admin", "admin@test.com", "password123")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	adminSession, err := svc.CreateSession(ctx, admin.ID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	if rec := loginAsRequest(t, h, admin.ID, admin, adminSession); rec.Code != http.StatusBadRequest {
		t.Errorf("expected self-impersonation to be rejected with 400, got %d", rec.Code)
	}

	inactive, err := svc.CreateUser(ctx, "ghost", "ghost@test.com", "password123")
	if err != nil {
		t.Fatalf("create inactive: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, inactive.ID, inactive.Username, inactive.Email, false); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}
	if rec := loginAsRequest(t, h, inactive.ID, admin, adminSession); rec.Code != http.StatusBadRequest {
		t.Errorf("expected inactive target to be rejected with 400, got %d", rec.Code)
	}
}
