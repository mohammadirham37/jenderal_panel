package dbmanager

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
)

// RevokePrivileges must fall back to the URL's user id when the body omits
// user_id — the databases page sends only database_id.
func TestRevokePrivilegesUsesURLUserIDWhenBodyOmitsIt(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, newMockExec(), audit.NewService(db))

	if _, err := db.Exec(
		`INSERT INTO managed_databases (id, name, engine, charset, created_by, created_at, updated_at)
		 VALUES ('db-1', 'appdb', 'mysql', 'utf8mb4', 'panel-user', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed database: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO db_users (id, username, engine, privileges, created_by, created_at, updated_at)
		 VALUES ('dbuser-1', 'app_user', 'mysql', '[]', 'panel-user', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed db user: %v", err)
	}

	h := NewHandler(svc, audit.NewService(db), nil)

	req := httptest.NewRequest("POST", "/api/v1/databases/users/dbuser-1/revoke", strings.NewReader(`{"database_id":"db-1"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "dbuser-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	// Admin caller: the ownership gate is not what this test exercises.
	req = req.WithContext(auth.WithAdminFlag(req.Context(), true))

	rec := httptest.NewRecorder()
	h.RevokePrivileges(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}
