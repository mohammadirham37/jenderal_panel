package cloudflared

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectRejectsInvalidToken(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"token":"garbage"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	h.Connect(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}
