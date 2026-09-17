package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
