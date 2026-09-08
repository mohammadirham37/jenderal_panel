package nodejs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstallRejectsUnsupportedVersionBeforeStartingRootTask(t *testing.T) {
	handler := NewHandler(NewService(nil, nil, nil), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodejs/install", strings.NewReader(`{"version":"20; touch /tmp/injected"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.Install(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}
