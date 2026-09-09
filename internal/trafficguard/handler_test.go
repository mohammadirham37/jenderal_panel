package trafficguard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyRejectsEnforcementWithoutConfirmation(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"mode":"balanced","confirm":false}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	h.Apply(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
