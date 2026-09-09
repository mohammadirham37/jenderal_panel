package notification

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestTelegramTransportErrorDoesNotExposeToken(t *testing.T) {
	previous := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})}
	t.Cleanup(func() { httpClient = previous })

	err := SendTelegram(`{"bot_token":"super-secret-token","chat_id":"123"}`, "test")
	if err == nil || strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("SendTelegram() error = %q; token must be redacted", err)
	}
}

func TestWebhookRejectsRedirectAndOtherNon2xxResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := SendWebhook(`{"url":"`+server.URL+`/redirect"}`, "test"); err == nil {
		t.Fatal("SendWebhook() accepted redirect as successful delivery")
	}
}
