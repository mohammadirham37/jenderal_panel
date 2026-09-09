package queue

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
	for _, bodyWebsiteID := range []string{"", "site-other"} {
		t.Run("body_website_id="+bodyWebsiteID, func(t *testing.T) {
			db := setupTestDB(t)
			insertTestWebsite(t, db, "site-route", "route-user")
			insertTestWebsite(t, db, "site-other", "other-user")
			handler := NewHandler(NewService(db, mockExecutor(), nil), audit.NewService(db))
			router := chi.NewRouter()
			router.Post("/websites/{id}/queue-workers", handler.CreateForWebsite)

			recorder := httptest.NewRecorder()
			body := `{"command":"php artisan queue:work","num_workers":2}`
			if bodyWebsiteID != "" {
				body = `{"website_id":"site-other","command":"php artisan queue:work","num_workers":2}`
			}
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/websites/site-route/queue-workers", strings.NewReader(body)))
			if recorder.Code != http.StatusCreated {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var response struct {
				Data model.QueueWorker `json:"data"`
			}
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			got := response.Data
			if got.WebsiteID != "site-route" {
				t.Fatalf("website_id = %q, want route website", got.WebsiteID)
			}
			var storedWebsiteID string
			if err := db.QueryRow(`SELECT website_id FROM queue_workers WHERE id = ?`, got.ID).Scan(&storedWebsiteID); err != nil {
				t.Fatal(err)
			}
			if storedWebsiteID != "site-route" {
				t.Fatalf("stored website_id = %q, want route website", storedWebsiteID)
			}
		})
	}
}

func TestHandlerCreateForWebsiteRejectsMissingWebsite(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-other", "other-user")
	handler := NewHandler(NewService(db, mockExecutor(), nil), audit.NewService(db))
	router := chi.NewRouter()
	router.Post("/websites/{id}/queue-workers", handler.CreateForWebsite)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/websites/missing-site/queue-workers", strings.NewReader(`{"website_id":"site-other","command":"php artisan queue:work","num_workers":2}`)))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", recorder.Code, recorder.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM queue_workers`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("created %d workers for a missing route website", count)
	}
}

func TestHandlerListForWebsiteReturnsOnlyRouteWebsiteWorkers(t *testing.T) {
	db := setupTestDB(t)
	insertTestWebsite(t, db, "site-route", "route-user")
	insertTestWebsite(t, db, "site-other", "other-user")
	if _, err := db.Exec(`INSERT INTO queue_workers (id, website_id, command, num_workers, auto_restart, status, created_at, updated_at)
		VALUES ('route-worker', 'site-route', 'route-command', 1, 1, 'running', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		       ('other-worker', 'site-other', 'other-command', 1, 1, 'running', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(NewService(db, mockExecutor(), nil), audit.NewService(db))
	router := chi.NewRouter()
	router.Get("/websites/{id}/queue-workers", handler.ListForWebsite)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/websites/site-route/queue-workers", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data []model.QueueWorker `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	got := response.Data
	if len(got) != 1 || got[0].ID != "route-worker" || got[0].WebsiteID != "site-route" {
		t.Fatalf("workers = %#v, want only route website worker", got)
	}
}
