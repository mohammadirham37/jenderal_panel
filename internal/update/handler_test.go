package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCurrentReportsRunningRevisionWithoutGitHub(t *testing.T) {
	const revision = "1234567890abcdef1234567890abcdef12345678"
	handler := NewHandler(NewService(nil, revision, nil), nil)
	recorder := httptest.NewRecorder()
	handler.Current(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/update/current", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response struct {
		Data struct {
			CurrentVersion string `json:"current_version"`
		} `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Data.CurrentVersion != "12345678" {
		t.Fatalf("current_version = %q, want 12345678", response.Data.CurrentVersion)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestCheckResponseDisablesCaching(t *testing.T) {
	const revision = "1234567890abcdef1234567890abcdef12345678"
	svc := NewService(nil, revision, nil)
	svc.httpClient = githubCommitClient(revision)
	handler := NewHandler(svc, nil)

	recorder := httptest.NewRecorder()
	handler.Check(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/update/check", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	const want = "no-store, no-cache, must-revalidate"
	if got := recorder.Header().Get("Cache-Control"); got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
}
