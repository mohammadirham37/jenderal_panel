package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/dbmanager"
	"github.com/mohammadirham37/jenderal_panel/internal/deployment"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/website"
)

type scopeFixture struct {
	db      *sql.DB
	router  http.Handler
	owner   model.Session
	other   model.Session
	admin   model.Session
	siteID  string
	dbID    string
	deplyID string
}

func newScopeFixture(t *testing.T) scopeFixture {
	t.Helper()
	db, err := database.Open(config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "panel.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	authSvc := auth.NewService(db, config.AuthConfig{SessionTTL: time.Hour, Argon2: config.Argon2Config{Memory: 8 * 1024, Iterations: 1, Parallelism: 1}})
	rbac := auth.NewRBAC(db)
	if err := rbac.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	session := func(name, role string) (model.User, model.Session) {
		user, err := authSvc.CreateUser(ctx, name, name+"@example.com", "Password-123!")
		if err != nil {
			t.Fatal(err)
		}
		if err := rbac.AssignRole(ctx, user.ID, role); err != nil {
			t.Fatal(err)
		}
		s, err := authSvc.CreateSession(ctx, user.ID, "127.0.0.1", "test")
		if err != nil {
			t.Fatal(err)
		}
		return user, s
	}
	owner, ownerSession := session("owner", "user")
	_, otherSession := session("other", "user")
	_, adminSession := session("boss", "admin")

	now := time.Now().UTC().Format(time.RFC3339)
	mustExec := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO websites (id, domain, app_type, document_root, web_user, status, created_by, created_at, updated_at)
		VALUES ('site-1', 'owned.example.com', 'static', '/home/web_owned/public', 'web_owned', 'active', ?, ?, ?)`, owner.ID, now, now)
	mustExec(`INSERT INTO managed_databases (id, name, engine, charset, created_by, created_at, updated_at)
		VALUES ('db-1', 'owned_db', 'mysql', 'utf8mb4', ?, ?, ?)`, owner.ID, now, now)
	mustExec(`INSERT INTO deployments (id, website_id, status, created_at, updated_at) VALUES ('dep-1', 'site-1', 'success', ?, ?)`, now, now)

	exec := &executor.MockExecutor{}
	router := NewRouter(Dependencies{
		AuthSvc:       authSvc,
		RBAC:          rbac,
		WebsiteSvc:    website.NewService(db, exec, nil),
		DBManagerSvc:  dbmanager.NewService(db, exec, nil),
		DeploymentSvc: deployment.NewService(db, exec, nil),
	})
	return scopeFixture{db: db, router: router, owner: ownerSession, other: otherSession, admin: adminSession,
		siteID: "site-1", dbID: "db-1", deplyID: "dep-1"}
}

func (f scopeFixture) status(t *testing.T, s model.Session, method, path string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: s.ID})
	rec := httptest.NewRecorder()
	func() {
		defer func() { _ = recover() }() // handlers with nil deps may panic after the scope check passed
		f.router.ServeHTTP(rec, req)
	}()
	return rec.Code
}

// The ownership check must run for the real /api/v1 routes: chi reports the
// full pattern ("/api/v1/websites/{id}"), not the sub-route.
func TestScopeMiddlewareBlocksOtherUsersOnAPIRoutes(t *testing.T) {
	f := newScopeFixture(t)
	for _, path := range []string{
		"/api/v1/websites/" + f.siteID,
		"/api/v1/websites/" + f.siteID + "/env",
		"/api/v1/databases/" + f.dbID + "/export",
		"/api/v1/deployments/" + f.deplyID,
	} {
		if code := f.status(t, f.other, http.MethodGet, path); code != http.StatusForbidden {
			t.Errorf("non-owner GET %s = %d, want 403", path, code)
		}
		for _, who := range []model.Session{f.owner, f.admin} {
			if code := f.status(t, who, http.MethodGet, path); code == http.StatusForbidden {
				t.Errorf("GET %s by %s = 403, owners and admins must pass the scope check", path, who.UserID)
			}
		}
	}
}
