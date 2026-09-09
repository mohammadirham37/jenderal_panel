package api

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestSecurityRoutesRegistered(t *testing.T) {
	router := NewRouter(Dependencies{})
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("router does not expose chi routes")
	}
	want := map[string]bool{
		http.MethodGet + " /api/v1/security/overview":                false,
		http.MethodGet + " /api/v1/security/events":                  false,
		http.MethodPost + " /api/v1/security/events/{id}/transition": false,
		http.MethodGet + " /api/v1/security/fail2ban":                false,
		http.MethodPost + " /api/v1/security/fail2ban/install":       false,
		http.MethodPut + " /api/v1/security/fail2ban/settings":       false,
		http.MethodGet + " /api/v1/security/fail2ban/bans":           false,
		http.MethodPost + " /api/v1/security/fail2ban/bans":          false,
		http.MethodDelete + " /api/v1/security/fail2ban/bans/{ip}":   false,
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
