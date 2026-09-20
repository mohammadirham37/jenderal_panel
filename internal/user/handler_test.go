package user

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

// The password length check runs before any database access, so a nil DB is
// fine for these tests.
func newPolicyTestHandler() *Handler {
	cfg := config.AuthConfig{
		Argon2: config.Argon2Config{Memory: 64 * 1024, Iterations: 1, Parallelism: 1},
	}
	return NewHandler(auth.NewService(nil, cfg), auth.NewRBAC(nil), audit.NewService(nil), nil)
}

func createWithPassword(t *testing.T, h *Handler, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := bytes.NewBufferString(`{"username":"tester","email":"t@example.com","password":` + quote(password) + `,"role":"user"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", body)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	return rec
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestCreateRejectsShortPassword(t *testing.T) {
	h := newPolicyTestHandler()
	if rec := createWithPassword(t, h, "short"); rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a %d-character password, got %d", len("short"), rec.Code)
	}
}

func TestValidatePasswordLength(t *testing.T) {
	if err := validatePasswordLength("short"); err == nil {
		t.Error("expected a 5-character password to be rejected")
	}
	if err := validatePasswordLength("12345678"); err != nil {
		t.Errorf("expected an 8-character password to pass, got %v", err)
	}
	if err := validatePasswordLength("long enough with spaces 1"); err != nil {
		t.Errorf("expected a long passphrase to pass, got %v", err)
	}
}
