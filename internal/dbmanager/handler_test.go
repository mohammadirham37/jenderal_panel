package dbmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/settings"
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

	h := NewHandler(svc, audit.NewService(db), nil, settings.NewService(db))

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

// The restore upload limit must default to 2 GB when the setting is unset,
// persist a new value across reads, and reject out-of-range values.
func TestRestoreLimitDefaultSetAndGet(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, newMockExec(), audit.NewService(db))
	h := NewHandler(svc, audit.NewService(db), nil, settings.NewService(db))

	getLimit := func() int64 {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/databases/restore-limit", nil)
		rec := httptest.NewRecorder()
		h.GetRestoreLimit(rec, req)
		if rec.Code != 200 {
			t.Fatalf("GET status = %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data struct {
				MaxMB int64 `json:"max_mb"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return body.Data.MaxMB
	}
	putLimit := func(maxMB int64) int {
		t.Helper()
		req := httptest.NewRequest("PUT", "/api/v1/databases/restore-limit",
			strings.NewReader(`{"max_mb":`+strconv.FormatInt(maxMB, 10)+`}`))
		rec := httptest.NewRecorder()
		h.SetRestoreLimit(rec, req)
		return rec.Code
	}

	if got := getLimit(); got != 2048 {
		t.Fatalf("default max_mb = %d, want 2048", got)
	}
	if code := putLimit(512); code != 200 {
		t.Fatalf("PUT 512 status = %d: %s", code, "expected ok")
	}
	if got := getLimit(); got != 512 {
		t.Fatalf("max_mb after PUT = %d, want 512", got)
	}
	for _, invalid := range []int64{0, -5, 102401} {
		if code := putLimit(invalid); code != 400 {
			t.Fatalf("PUT %d status = %d, want 400", invalid, code)
		}
	}
	if got := getLimit(); got != 512 {
		t.Fatalf("max_mb moved to %d after rejected PUTs, want 512", got)
	}
}

// Saving the limit must fire the notifier so the panel domain vhost refresh
// picks up the new nginx body size immediately.
func TestSetRestoreLimitFiresNotifier(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, newMockExec(), audit.NewService(db))
	h := NewHandler(svc, audit.NewService(db), nil, settings.NewService(db))

	calls := 0
	h.SetRestoreLimitNotifier(func(ctx context.Context) { calls++ })

	req := httptest.NewRequest("PUT", "/api/v1/databases/restore-limit", strings.NewReader(`{"max_mb":1024}`))
	rec := httptest.NewRecorder()
	h.SetRestoreLimit(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Fatalf("notifier calls = %d, want 1", calls)
	}
}

// An upload larger than the configured limit must be rejected with 413
// before any part of the dump is processed.
func TestRestoreRejectsUploadOverConfiguredLimit(t *testing.T) {
	db := setupTestDB(t)
	settingsSvc := settings.NewService(db)
	svc := NewService(db, newMockExec(), audit.NewService(db))
	h := NewHandler(svc, audit.NewService(db), nil, settingsSvc)

	if err := settingsSvc.Set(context.Background(), restoreMaxMBSetting, "1"); err != nil {
		t.Fatalf("set limit: %v", err)
	}

	// The body must be a well-formed multipart form whose file part exceeds
	// the limit: a malformed body fails ParseMultipartForm before
	// MaxBytesReader can surface its error, yielding 400 instead of 413.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "dump.sql")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), 2<<20)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/v1/databases/db-1/restore", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "db-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.RestoreDatabase(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "restore file exceeds the 1 MB limit") {
		t.Errorf("error should name the configured 1 MB limit: %s", rec.Body.String())
	}
}
