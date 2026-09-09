package cron

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestHandlerCreateForWebsiteUsesRouteWebsiteID(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-route", "route-user")
	handler := NewHandler(NewService(db, mockExecutor(), nil), audit.NewService(db))
	router := chi.NewRouter()
	router.Post("/websites/{id}/cron-jobs", handler.CreateForWebsite)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/websites/site-route/cron-jobs", strings.NewReader(`{"command":"php artisan schedule:run","schedule":"* * * * *"}`)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data model.CronJob `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	got := response.Data
	if got.WebsiteID != "site-route" {
		t.Fatalf("website_id = %q, want route website", got.WebsiteID)
	}
}

func TestHandlerListForWebsiteReturnsOnlyRouteWebsiteJobs(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-route", "route-user")
	insertTestWebsite(t, db, "site-other", "other-user")
	if _, err := db.Exec(`INSERT INTO cron_jobs (id, website_id, command, schedule, enabled, created_at, updated_at)
		VALUES ('route-job', 'site-route', 'route-command', '* * * * *', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		       ('other-job', 'site-other', 'other-command', '* * * * *', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(NewService(db, mockExecutor(), nil), audit.NewService(db))
	router := chi.NewRouter()
	router.Get("/websites/{id}/cron-jobs", handler.ListForWebsite)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/websites/site-route/cron-jobs", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data []model.CronJob `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	got := response.Data
	if len(got) != 1 || got[0].ID != "route-job" || got[0].WebsiteID != "site-route" {
		t.Fatalf("jobs = %#v, want only route website job", got)
	}
}
