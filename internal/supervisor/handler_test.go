package supervisor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateRejectsBadJSONBody(t *testing.T) {
	h := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.Create(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
