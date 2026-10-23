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
	if method != http.MethodGet {
		const csrf = "test-csrf-token"
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
		req.Header.Set("X-CSRF-Token", csrf)
	}
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

func TestResolveTerminalUser(t *testing.T) {
	f := newScopeFixture(t)
	d := Dependencies{WebsiteSvc: website.NewService(f.db, &executor.MockExecutor{}, nil)}
	ctx := context.Background()
	var ownerID string
	if err := f.db.QueryRow(`SELECT created_by FROM websites WHERE id = 'site-1'`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name            string
		admin           bool
		userID          string
		websiteID, user string
		wantUser        string
		wantErr         bool
	}{
		{name: "owner by web_user (the website page)", userID: ownerID, user: "web_owned", wantUser: "web_owned"},
		{name: "owner by website_id", userID: ownerID, websiteID: "site-1", wantUser: "web_owned"},
		{name: "owner website_id with matching web_user", userID: ownerID, websiteID: "site-1", user: "web_owned", wantUser: "web_owned"},
		{name: "owner website_id with web_user=root", userID: ownerID, websiteID: "site-1", user: "root", wantErr: true},
		{name: "owner website_id with another account", userID: ownerID, websiteID: "site-1", user: "web_other", wantErr: true},
		{name: "owner unscoped root shell", userID: ownerID, wantErr: true},
		{name: "owner web_user=root without website", userID: ownerID, user: "root", wantErr: true},
		{name: "other user on owned site", userID: "someone-else", websiteID: "site-1", wantErr: true},
		{name: "admin unscoped root shell", admin: true, userID: "admin", wantUser: ""},
		{name: "admin on a site uses the site account", admin: true, userID: "admin", websiteID: "site-1", wantUser: "web_owned"},
		{name: "admin web_user=root is still refused", admin: true, userID: "admin", user: "root", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := d.resolveTerminalUser(ctx, tc.admin, tc.userID, tc.websiteID, tc.user)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got shell user %q, want refusal", got)
				}
				return
			}
			if err != nil || got != tc.wantUser {
				t.Fatalf("got %q, %v; want %q", got, err, tc.wantUser)
			}
		})
	}
}

// The raw vhost editor writes nginx configuration that the root nginx master
// then loads, so it is admin-only even though owners may change their site.
func TestRawVhostEditorIsAdminOnly(t *testing.T) {
	f := newScopeFixture(t)
	path := "/api/v1/websites/" + f.siteID + "/config"
	if code := f.status(t, f.owner, http.MethodPut, path); code != http.StatusForbidden {
		t.Errorf("site owner PUT %s = %d, want 403", path, code)
	}
	if code := f.status(t, f.admin, http.MethodPut, path); code == http.StatusForbidden {
		t.Errorf("admin PUT %s = 403, admins must keep the editor", path)
	}
	// Reading the generated config stays available to the owner.
	if code := f.status(t, f.owner, http.MethodGet, path); code == http.StatusForbidden {
		t.Errorf("site owner GET %s = 403, want the config to stay readable", path)
	}
}

// Server logs and the global task list expose other tenants' and the
// admin's activity, so they are not open to every signed-in account.
func TestServerLogsAndTaskListAreRestricted(t *testing.T) {
	f := newScopeFixture(t)
	for _, path := range []string{"/api/v1/logs?path=/var/log/syslog", "/api/v1/tasks"} {
		if code := f.status(t, f.other, http.MethodGet, path); code != http.StatusForbidden {
			t.Errorf("user-role GET %s = %d, want 403", path, code)
		}
		if code := f.status(t, f.admin, http.MethodGet, path); code == http.StatusForbidden {
			t.Errorf("admin GET %s = 403, admins must keep access", path)
		}
	}
	// A task the caller holds the id of stays readable, so progress views
	// keep working for every role.
	if code := f.status(t, f.other, http.MethodGet, "/api/v1/tasks/does-not-exist"); code == http.StatusForbidden {
		t.Error("reading a task by id must stay available")
	}
}

// The database engines are shared by every tenant, so their lifecycle is
// not part of the user role's own-database permissions.
func TestDatabaseEngineControlIsAdminOnly(t *testing.T) {
	f := newScopeFixture(t)
	for _, path := range []string{
		"/api/v1/databases/engines/mysql/stop",
		"/api/v1/databases/engines/mysql/restart",
		"/api/v1/databases/engines/postgresql/install",
	} {
		if code := f.status(t, f.other, http.MethodPost, path); code != http.StatusForbidden {
			t.Errorf("user-role POST %s = %d, want 403", path, code)
		}
		if code := f.status(t, f.admin, http.MethodPost, path); code == http.StatusForbidden {
			t.Errorf("admin POST %s = 403, admins must keep engine control", path)
		}
	}
	// Owners keep managing their own databases.
	if code := f.status(t, f.owner, http.MethodPost, "/api/v1/databases/"+f.dbID+"/restore"); code == http.StatusForbidden {
		t.Error("the database owner must still be allowed to restore it")
	}
}
