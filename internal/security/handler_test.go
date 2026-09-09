package security

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

func TestTransitionEventRejectsUnknownStatus(t *testing.T) {
	events, _ := newEventTestService(t, nil)
	handler := NewHandler(NewService(events, taskrunner.New()), events, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/security/events/01/transition", bytes.NewBufferString(`{"status":"deleted"}`))
	r.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "01")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	handler.TransitionEvent(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTransitionEventAuditsSuccessfulMutation(t *testing.T) {
	events, db := newEventTestService(t, nil)
	event, _, err := events.Record(context.Background(), validEventInput(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(NewService(events, taskrunner.New()), events, audit.NewService(db))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/security/events/"+event.ID+"/transition", bytes.NewBufferString(`{"status":"acknowledged"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", event.ID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	handler.TransitionEvent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE module = 'security' AND target = ?`, event.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("security audit entries = %d, want 1", count)
	}
}
