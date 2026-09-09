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

func TestRequestConstructionErrorsDoNotExposeCredentials(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		send   func() error
	}{
		{
			name:   "telegram token",
			secret: "123:%secret",
			send: func() error {
				return SendTelegram(`{"bot_token":"123:%secret","chat_id":"123"}`, "test")
			},
		},
		{
			name:   "discord webhook",
			secret: "credential%secret",
			send: func() error {
				return SendDiscord(`{"webhook_url":"https://discord.com/api/webhooks/credential%secret"}`, "test")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.send()
			if err == nil || strings.Contains(err.Error(), tt.secret) {
				t.Fatalf("delivery error = %q; credential must be rejected without disclosure", err)
			}
		})
	}
}

func TestValidateConfigRejectsMalformedTelegramToken(t *testing.T) {
	err := ValidateConfig("telegram", `{"bot_token":"123:%secret","chat_id":"123"}`)
	if err == nil || strings.Contains(err.Error(), "123:%secret") {
		t.Fatalf("ValidateConfig() error = %q; malformed token must be rejected safely", err)
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
