package ssl

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
)

func TestHandlerInstallCustomKeepsMaterialOutOfResponseAndAudit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-handler", "example.com")
	now := time.Now().UTC().Truncate(time.Second)
	certPEM, keyPEM := testCertificate(t, []string{"example.com"}, now.Add(-time.Hour), now.Add(24*time.Hour))

	auditSvc := audit.NewService(db)
	handler := NewHandler(newTestService(t, db, &MockACMEClient{}), auditSvc)
	body, err := json.Marshal(map[string]string{
		"website_id": "ws-handler", "domain": "example.com",
		"certificate_pem": string(certPEM), "private_key_pem": string(keyPEM),
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.InstallCustom(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/ssl/custom", bytes.NewReader(body)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := recorder.Body.String()
	if strings.Contains(response, string(certPEM)) || strings.Contains(response, string(keyPEM)) || strings.Contains(response, "certificate_pem") || strings.Contains(response, "private_key_pem") {
		t.Fatal("response exposed certificate material")
	}
	var detail string
	if err := db.QueryRow(`SELECT detail FROM audit_logs WHERE action = 'install_custom_ssl'`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "example.com") || !strings.Contains(detail, "custom") {
		t.Fatalf("audit detail = %q, want domain and issuer", detail)
	}
	if strings.Contains(detail, string(certPEM)) || strings.Contains(detail, string(keyPEM)) {
		t.Fatal("audit detail exposed certificate material")
	}
}

func TestHandlerInstallCustomRejectsOversizedBody(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	handler := NewHandler(newTestService(t, db, &MockACMEClient{}), audit.NewService(db))
	payload := `{"website_id":"ws","domain":"example.com","certificate_pem":"` + strings.Repeat("A", (512<<10)+1) + `","private_key_pem":"KEY"}`
	recorder := httptest.NewRecorder()
	handler.InstallCustom(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/ssl/custom", strings.NewReader(payload)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerInstallCustomRejectsMalformedMaterial(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-invalid", "example.com")
	handler := NewHandler(newTestService(t, db, &MockACMEClient{}), audit.NewService(db))
	body := `{"website_id":"ws-invalid","domain":"example.com","certificate_pem":"bad","private_key_pem":"bad"}`
	recorder := httptest.NewRecorder()
	handler.InstallCustom(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/ssl/custom", strings.NewReader(body)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerUpdateAutoRenew(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-update", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range []struct{ id, issuer string }{{"cert-le", "letsencrypt"}, {"cert-custom", "custom"}} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-update', 'example.com', ?, 'active', 0, ?, ?)`, item.id, item.issuer, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(newTestService(t, db, &MockACMEClient{}), audit.NewService(db))
	router := chi.NewRouter()
	router.Put("/ssl/{id}", handler.Update)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/ssl/cert-le", strings.NewReader(`{"auto_renew":true}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("letsencrypt status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var enabled int
	if err := db.QueryRow(`SELECT auto_renew FROM ssl_certificates WHERE id = 'cert-le'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("auto_renew = %d, want 1", enabled)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/ssl/cert-custom", strings.NewReader(`{"auto_renew":true}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("custom status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
