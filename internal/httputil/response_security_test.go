package httputil

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func TestDecodeJSONRejectsOversizedBodies(t *testing.T) {
	huge := `{"password":"` + strings.Repeat("a", int(MaxJSONBody)+1024) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(huge))
	var body struct {
		Password string `json:"password"`
	}
	err := DecodeJSON(req, &body)
	if err == nil {
		t.Fatal("an oversized body was accepted")
	}
	var domainErr *model.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("err = %v, want a validation error", err)
	}

	// Ordinary bodies still decode.
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"password":"short"}`))
	if err := DecodeJSON(req, &body); err != nil || body.Password != "short" {
		t.Fatalf("normal body: %v (%q)", err, body.Password)
	}
}

// Internal failures must not hand the caller command output, SQL text, or
// file paths; domain errors keep their actionable message.
func TestHandleErrorHidesInternalDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	HandleError(rec, fmt.Errorf("query session: %w", errors.New("no such column: secret in /var/lib/jenderal/panel.db")))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, leak := range []string{"no such column", "/var/lib/jenderal"} {
		if strings.Contains(body, leak) {
			t.Errorf("response leaks internal detail %q: %s", leak, body)
		}
	}

	rec = httptest.NewRecorder()
	HandleError(rec, model.NewValidationError("domain is required"))
	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error.Message != "domain is required" {
		t.Errorf("domain error message changed: %q", resp.Error.Message)
	}
}
