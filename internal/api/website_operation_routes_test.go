package api

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestWebsiteOperationRoutesRegistered(t *testing.T) {
	router := NewRouter(Dependencies{})
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("router does not expose chi routes")
	}
	want := map[string]bool{
		http.MethodGet + " /api/v1/websites/{id}/ssl":              false,
		http.MethodPost + " /api/v1/websites/{id}/ssl/issue":       false,
		http.MethodPost + " /api/v1/websites/{id}/ssl/custom":      false,
		http.MethodGet + " /api/v1/websites/{id}/cron-jobs":        false,
		http.MethodPost + " /api/v1/websites/{id}/cron-jobs":       false,
		http.MethodGet + " /api/v1/websites/{id}/queue-workers":    false,
		http.MethodPost + " /api/v1/websites/{id}/queue-workers":   false,
		http.MethodPost + " /api/v1/websites/{id}/app/{action}":    false,
		http.MethodPost + " /api/v1/websites/{id}/octane/enable":   false,
		http.MethodPost + " /api/v1/websites/{id}/octane/reload":   false,
		http.MethodPost + " /api/v1/websites/{id}/octane/{action}": false,
		http.MethodGet + " /api/v1/ssl":                            false,
		http.MethodPost + " /api/v1/cron-jobs":                     false,
		http.MethodPost + " /api/v1/queue-workers":                 false,
	}
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		if _, exists := want[key]; exists {
			want[key] = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for route, found := range want {
		if !found {
			t.Errorf("route %s not registered", route)
		}
	}
}
