package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
