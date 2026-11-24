package depaudit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerScanAndReport(t *testing.T) {
	s, f, _ := newTestService(t)
	h := NewHandler(s, nil)

	f.mu.Lock() // keep the scan running for the second request
	rec := httptest.NewRecorder()
	h.Scan(rec, httptest.NewRequest(http.MethodPost, "/api/v1/security/dependencies/scan", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("scan status = %d %s", rec.Code, rec.Body)
	}
	again := httptest.NewRecorder()
	h.Scan(again, httptest.NewRequest(http.MethodPost, "/api/v1/security/dependencies/scan", nil))
	f.mu.Unlock()
	if again.Code != http.StatusBadRequest && again.Code != http.StatusUnprocessableEntity {
		t.Errorf("second scan status = %d, want a validation error", again.Code)
	}
	s.wait()

	rec = httptest.NewRecorder()
	h.Report(rec, httptest.NewRequest(http.MethodGet, "/api/v1/security/dependencies", nil))
	var body struct {
		Data Report `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Scan == nil || body.Data.Scan.Status != StatusCompleted || len(body.Data.Findings) != 4 {
		t.Errorf("report = %+v", body.Data)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("report must not be cached")
	}
}
