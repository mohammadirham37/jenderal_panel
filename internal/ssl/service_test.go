package ssl

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// setupTestDB creates an in-memory SQLite database with all required tables.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	if err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS websites (
		id            TEXT PRIMARY KEY,
		domain        TEXT NOT NULL UNIQUE,
		app_type      TEXT NOT NULL DEFAULT 'php',
		php_version   TEXT,
		document_root TEXT NOT NULL,
		web_user      TEXT NOT NULL UNIQUE,
		status        TEXT NOT NULL DEFAULT 'pending',
		error_message TEXT,
		ssl_enabled   INTEGER NOT NULL DEFAULT 0,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS domains (
		id         TEXT PRIMARY KEY,
		website_id TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
		name       TEXT NOT NULL UNIQUE,
		type       TEXT NOT NULL DEFAULT 'alias',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS ssl_certificates (
		id            TEXT PRIMARY KEY,
		website_id    TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
		domain        TEXT NOT NULL,
		issuer        TEXT NOT NULL DEFAULT 'letsencrypt',
		status        TEXT NOT NULL DEFAULT 'pending',
		expires_at    TEXT,
		auto_renew    INTEGER NOT NULL DEFAULT 1,
		error_message TEXT,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS audit_logs (
		id         TEXT PRIMARY KEY,
		user_id    TEXT,
		action     TEXT NOT NULL,
		module     TEXT NOT NULL,
		target     TEXT,
		detail     TEXT,
		ip_address TEXT,
		created_at TEXT NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create tables: %v", err)
	}

	return db
}

// insertTestWebsite inserts a website record into the test database.
func insertTestWebsite(t *testing.T, db *sql.DB, id, domain string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	webUser := "web_" + domain
	docRoot := "/home/" + webUser + "/public"

	_, err := db.Exec(
		`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, ssl_enabled, created_at, updated_at)
		 VALUES (?, ?, 'php', '8.2', ?, ?, 'active', 0, ?, ?)`,
		id, domain, docRoot, webUser, now, now,
	)
	if err != nil {
		t.Fatalf("insert website: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO domains (id, website_id, name, type, created_at) VALUES (?, ?, ?, 'primary', ?)`,
		"dom-"+id, id, domain, now,
	)
	if err != nil {
		t.Fatalf("insert domain: %v", err)
	}
}

// newMockExecutor returns a MockExecutor that succeeds for all commands.
func newMockExecutor() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Duration: time.Millisecond}, nil
		},
	}
}

// newTestService creates a Service with mock dependencies for testing.
func newTestService(t *testing.T, db *sql.DB, acme ACMEClient) *Service {
	t.Helper()
	mock := newMockExecutor()
	auditSvc := audit.NewService(db)
	return NewService(db, mock, auditSvc, acme, "/tmp/jenderal_ssl_test")
}

// fakeCertPEM and fakeKeyPEM are dummy PEM bytes for testing.
var (
	fakeCertPEM = []byte("-----BEGIN CERTIFICATE-----\nfake-cert-data\n-----END CERTIFICATE-----\n")
	fakeKeyPEM  = []byte("-----BEGIN RSA PRIVATE KEY-----\nfake-key-data\n-----END RSA PRIVATE KEY-----\n")
)

func TestIssue_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-001", "example.com")

	acme := &MockACMEClient{
		ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
			if domain != "example.com" {
				t.Errorf("unexpected domain: %s", domain)
			}
			return fakeCertPEM, fakeKeyPEM, nil
		},
		RevokeFunc: func(certPEM []byte) error {
			return nil
		},
	}

	svc := newTestService(t, db, acme)

	cert, err := svc.Issue(context.Background(), "ws-001", "example.com")
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	if cert.Status != "active" {
		t.Errorf("expected status 'active', got %q", cert.Status)
	}
	if cert.Domain != "example.com" {
		t.Errorf("expected domain 'example.com', got %q", cert.Domain)
	}
	if cert.WebsiteID != "ws-001" {
		t.Errorf("expected website_id 'ws-001', got %q", cert.WebsiteID)
	}
	if cert.Issuer != "letsencrypt" {
		t.Errorf("expected issuer 'letsencrypt', got %q", cert.Issuer)
	}
	if cert.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at")
	}
	if !cert.AutoRenew {
		t.Error("expected auto_renew to be true")
	}

	// Verify DB record.
	dbCert, err := svc.Get(context.Background(), cert.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if dbCert.Status != "active" {
		t.Errorf("DB status: expected 'active', got %q", dbCert.Status)
	}

	// Verify website ssl_enabled was set.
	var sslEnabled int
	err = db.QueryRow(`SELECT ssl_enabled FROM websites WHERE id = ?`, "ws-001").Scan(&sslEnabled)
	if err != nil {
		t.Fatalf("query ssl_enabled: %v", err)
	}
	if sslEnabled != 1 {
		t.Errorf("expected ssl_enabled=1, got %d", sslEnabled)
	}
}

func TestIssue_ACMEFail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-002", "fail.example.com")

	acme := &MockACMEClient{
		ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
			return nil, nil, &acmeError{msg: "ACME challenge failed"}
		},
		RevokeFunc: func(certPEM []byte) error {
			return nil
		},
	}

	svc := newTestService(t, db, acme)

	cert, err := svc.Issue(context.Background(), "ws-002", "fail.example.com")
	if err != nil {
		t.Fatalf("Issue returned unexpected error: %v", err)
	}

	if cert.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", cert.Status)
	}
	if cert.ErrorMessage == "" {
		t.Error("expected non-empty error_message")
	}

	// Verify DB record.
	dbCert, err := svc.Get(context.Background(), cert.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if dbCert.Status != "failed" {
		t.Errorf("DB status: expected 'failed', got %q", dbCert.Status)
	}
	if dbCert.ErrorMessage == "" {
		t.Error("DB error_message should not be empty")
	}
}

func TestList(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-003", "list1.example.com")
	insertTestWebsite(t, db, "ws-004", "list2.example.com")

	acme := &MockACMEClient{
		ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
			return fakeCertPEM, fakeKeyPEM, nil
		},
		RevokeFunc: func(certPEM []byte) error {
			return nil
		},
	}

	svc := newTestService(t, db, acme)

	// Issue two certificates.
	_, err := svc.Issue(context.Background(), "ws-003", "list1.example.com")
	if err != nil {
		t.Fatalf("Issue 1 returned error: %v", err)
	}
	_, err = svc.Issue(context.Background(), "ws-004", "list2.example.com")
	if err != nil {
		t.Fatalf("Issue 2 returned error: %v", err)
	}

	certs, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(certs) != 2 {
		t.Errorf("expected 2 certificates, got %d", len(certs))
	}

	// Test ListByWebsite.
	wsCerts, err := svc.ListByWebsite(context.Background(), "ws-003")
	if err != nil {
		t.Fatalf("ListByWebsite returned error: %v", err)
	}
	if len(wsCerts) != 1 {
		t.Errorf("expected 1 certificate for ws-003, got %d", len(wsCerts))
	}
}

func TestGetExpiringCerts(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-005", "expiring.example.com")
	insertTestWebsite(t, db, "ws-006", "notexpiring.example.com")

	now := time.Now().UTC()

	// Insert a cert expiring in 7 days (should be returned for 14-day window).
	expiresIn7Days := now.Add(7 * 24 * time.Hour).Format(time.RFC3339)
	nowStr := now.Format(time.RFC3339)

	_, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, 'letsencrypt', 'active', ?, 1, ?, ?)`,
		"cert-exp-7", "ws-005", "expiring.example.com", expiresIn7Days, nowStr, nowStr,
	)
	if err != nil {
		t.Fatalf("insert expiring cert: %v", err)
	}

	// Insert a cert expiring in 30 days (should NOT be returned for 14-day window).
	expiresIn30Days := now.Add(30 * 24 * time.Hour).Format(time.RFC3339)
	_, err = db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, 'letsencrypt', 'active', ?, 1, ?, ?)`,
		"cert-exp-30", "ws-006", "notexpiring.example.com", expiresIn30Days, nowStr, nowStr,
	)
	if err != nil {
		t.Fatalf("insert non-expiring cert: %v", err)
	}

	acme := &MockACMEClient{
		ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
			return fakeCertPEM, fakeKeyPEM, nil
		},
		RevokeFunc: func(certPEM []byte) error {
			return nil
		},
	}

	svc := newTestService(t, db, acme)

	certs, err := svc.GetExpiringCerts(context.Background(), 14)
	if err != nil {
		t.Fatalf("GetExpiringCerts returned error: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("expected 1 expiring cert, got %d", len(certs))
	}
	if certs[0].ID != "cert-exp-7" {
		t.Errorf("expected cert id 'cert-exp-7', got %q", certs[0].ID)
	}
}

func TestCount(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-007", "count.example.com")

	acme := &MockACMEClient{
		ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
			return fakeCertPEM, fakeKeyPEM, nil
		},
		RevokeFunc: func(certPEM []byte) error {
			return nil
		},
	}

	svc := newTestService(t, db, acme)

	count, err := svc.Count(context.Background())
	if err != nil {
		t.Fatalf("Count returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}

	_, err = svc.Issue(context.Background(), "ws-007", "count.example.com")
	if err != nil {
		t.Fatalf("Issue returned error: %v", err)
	}

	count, err = svc.Count(context.Background())
	if err != nil {
		t.Fatalf("Count returned error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}
}

// acmeError is a simple error type for testing ACME failures.
type acmeError struct {
	msg string
}

func (e *acmeError) Error() string {
	return e.msg
}
