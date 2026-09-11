package ssl

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	websiteconfig "github.com/mohammadirham37/jenderal_panel/internal/website"
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
		node_version  TEXT NOT NULL DEFAULT '24',
		document_root TEXT NOT NULL,
		web_user      TEXT NOT NULL UNIQUE,
		status        TEXT NOT NULL DEFAULT 'pending',
		error_message TEXT,
		ssl_enabled   INTEGER NOT NULL DEFAULT 0,
		framework TEXT NOT NULL DEFAULT 'none',
		framework_version TEXT NOT NULL DEFAULT '',
		frontend_stack TEXT NOT NULL DEFAULT '',
		inertia_adapter TEXT NOT NULL DEFAULT '',
		project_variant TEXT NOT NULL DEFAULT 'empty',
		setup_mode TEXT NOT NULL DEFAULT 'config-only',
		provision_stage TEXT NOT NULL DEFAULT '',
		provision_log TEXT NOT NULL DEFAULT '',
		nginx_profile TEXT NOT NULL DEFAULT '',
		octane_enabled INTEGER NOT NULL DEFAULT 0,
		octane_port INTEGER NOT NULL DEFAULT 0,
		octane_workers INTEGER NOT NULL DEFAULT 4,
		created_by TEXT NOT NULL DEFAULT '',
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

func TestIssue_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-001", "example.com")
	issuedAt := time.Now().UTC().Truncate(time.Second)
	wantNotAfter := issuedAt.Add(60 * 24 * time.Hour)
	issuedCert, issuedKey := testCertificate(t, []string{"example.com"}, issuedAt.Add(-time.Hour), wantNotAfter)

	acme := &MockACMEClient{
		ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
			if domain != "example.com" {
				t.Errorf("unexpected domain: %s", domain)
			}
			if webroot != websiteconfig.DefaultACMEChallengeRoot {
				t.Errorf("webroot = %q, want %q", webroot, websiteconfig.DefaultACMEChallengeRoot)
			}
			return issuedCert, issuedKey, nil
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
	if !cert.ExpiresAt.Equal(wantNotAfter) {
		t.Errorf("expires_at = %v, want %v", cert.ExpiresAt, wantNotAfter)
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

func TestIssueMarksPendingRecordFailedWhenRequestEndsAfterACME(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-issue-canceled", "example.com")
	issuedAt := time.Now().UTC().Truncate(time.Second)
	issuedCert, issuedKey := testCertificate(t, []string{"example.com"}, issuedAt.Add(-time.Hour), issuedAt.Add(24*time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	svc := newTestService(t, db, &MockACMEClient{ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
		cancel()
		return issuedCert, issuedKey, nil
	}})
	if _, err := svc.Issue(ctx, "ws-issue-canceled", "example.com"); err == nil {
		t.Fatal("Issue() error = nil, want canceled revalidation")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM ssl_certificates WHERE website_id = 'ws-issue-canceled'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("certificate status = %q, want failed", status)
	}
}

func TestRecoverPendingReplacementRestoresPreviousRecord(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-recover-replacement", "example.com")
	now := time.Now().UTC().Truncate(time.Second)
	previous := model.SSLCertificate{
		ID: "cert-recover", WebsiteID: "ws-recover-replacement", Domain: "example.com",
		Issuer: "custom", Status: "active", ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'issuing', ?, 0, ?, ?)`,
		previous.ID, previous.WebsiteID, previous.Domain, previous.Issuer,
		previous.ExpiresAt.Format(time.RFC3339), previous.CreatedAt.Format(time.RFC3339), now.Format(time.RFC3339),
	); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, db, &MockACMEClient{})
	if err := svc.recoverPendingFailure(previous.ID, context.Canceled, previous, true); err != context.Canceled {
		t.Fatalf("recoverPendingFailure() error = %v, want context canceled", err)
	}
	restored, err := svc.Get(context.Background(), previous.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != "active" || restored.Issuer != "custom" || !restored.ExpiresAt.Equal(previous.ExpiresAt) {
		t.Fatalf("restored certificate = %#v", restored)
	}
}

func TestIssueSerializesWebsiteRequests(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-concurrent", "example.com")
	createdAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO domains (id, website_id, name, type, created_at) VALUES ('domain-concurrent-alias', 'ws-concurrent', 'www.example.com', 'alias', ?)`,
		createdAt,
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	primaryCert, primaryKey := testCertificate(t, []string{"example.com"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	aliasCert, aliasKey := testCertificate(t, []string{"www.example.com"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	acme := &MockACMEClient{ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
		entered <- struct{}{}
		<-release
		if domain == "www.example.com" {
			return aliasCert, aliasKey, nil
		}
		return primaryCert, primaryKey, nil
	}}
	svc := newTestService(t, db, acme)
	results := make(chan error, 2)
	go func() {
		_, err := svc.Issue(context.Background(), "ws-concurrent", "example.com")
		results <- err
	}()
	<-entered
	go func() {
		_, err := svc.Issue(context.Background(), "ws-concurrent", "www.example.com")
		results <- err
	}()
	concurrent := false
	select {
	case <-entered:
		concurrent = true
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	err1, err2 := <-results, <-results
	if concurrent {
		t.Fatal("SSL operations for the same website ran concurrently")
	}
	if err1 != nil || err2 != nil {
		t.Fatalf("request errors = %v and %v, want both successful", err1, err2)
	}
}

func TestIssueRollsBackNginxWhenDatabaseActivationFails(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-db-failure", "example.com")
	if _, err := db.Exec(`
		CREATE TRIGGER fail_ssl_state BEFORE UPDATE OF ssl_enabled ON websites
		BEGIN SELECT RAISE(FAIL, 'forced ssl state failure'); END;
	`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	certPEM, keyPEM := testCertificate(t, []string{"example.com"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	var restored bool
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" {
				return &executor.Result{ExitCode: 0}, nil
			}
			if name == "cp" && len(args) == 3 && args[0] == "-a" && strings.Contains(args[1], "jenderal_ssl_backup_") {
				restored = true
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, &MockACMEClient{ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
		return certPEM, keyPEM, nil
	}}, t.TempDir())
	if _, err := svc.Issue(context.Background(), "ws-db-failure", "example.com"); err == nil {
		t.Fatal("Issue() error = nil, want database activation failure")
	}
	if !restored {
		t.Fatal("Nginx and certificate files were not rolled back")
	}
	var status string
	var enabled int
	if err := db.QueryRow(`SELECT status FROM ssl_certificates WHERE website_id = 'ws-db-failure'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT ssl_enabled FROM websites WHERE id = 'ws-db-failure'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || enabled != 0 {
		t.Fatalf("status = %q, ssl_enabled = %d; want failed and 0", status, enabled)
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

func TestLoadSiteForDomain(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-domain", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO domains (id, website_id, name, type, created_at) VALUES (?, ?, ?, 'alias', ?)`,
		"dom-alias", "ws-domain", "www.example.com", now,
	); err != nil {
		t.Fatalf("insert alias: %v", err)
	}

	svc := newTestService(t, db, &MockACMEClient{})
	site, err := svc.loadSiteForDomain(context.Background(), "ws-domain", "www.example.com")
	if err != nil {
		t.Fatalf("loadSiteForDomain() error = %v", err)
	}
	if site.PrimaryDomain != "example.com" || site.Domain != "www.example.com" {
		t.Fatalf("unexpected site: %#v", site)
	}
	if len(site.Aliases) != 1 || site.Aliases[0] != "www.example.com" {
		t.Fatalf("aliases = %#v, want www.example.com", site.Aliases)
	}

	if _, err := svc.loadSiteForDomain(context.Background(), "ws-domain", "other.example.net"); err == nil {
		t.Fatal("unregistered domain was accepted")
	}
}

func TestList(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestWebsite(t, db, "ws-003", "list1.example.com")
	insertTestWebsite(t, db, "ws-004", "list2.example.com")
	insertTestWebsite(t, db, "ws-empty", "empty.example.com")

	acme := &MockACMEClient{
		ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
			now := time.Now().UTC().Truncate(time.Second)
			certPEM, keyPEM := testCertificate(t, []string{domain}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
			return certPEM, keyPEM, nil
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
	if len(wsCerts) == 1 && wsCerts[0].WebsiteID != "ws-003" {
		t.Errorf("certificate website_id = %q, want ws-003", wsCerts[0].WebsiteID)
	}
	emptyCerts, err := svc.ListByWebsite(context.Background(), "ws-empty")
	if err != nil || len(emptyCerts) != 0 {
		t.Fatalf("empty website certs = %#v, error = %v, want empty result", emptyCerts, err)
	}
	_, err = svc.ListByWebsite(context.Background(), "missing-site")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("error = %v, want model.ErrNotFound", err)
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
			now := time.Now().UTC().Truncate(time.Second)
			certPEM, keyPEM := testCertificate(t, []string{domain}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
			return certPEM, keyPEM, nil
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
			now := time.Now().UTC().Truncate(time.Second)
			certPEM, keyPEM := testCertificate(t, []string{domain}, now.Add(-time.Hour), now.Add(60*24*time.Hour))
			return certPEM, keyPEM, nil
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

func TestInstallCustom(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-custom", "example.com")

	now := time.Now().UTC().Truncate(time.Second)
	wantNotAfter := now.Add(45 * 24 * time.Hour)
	certPEM, keyPEM := testCertificate(t, []string{"example.com"}, now.Add(-time.Hour), wantNotAfter)
	svc := newTestService(t, db, &MockACMEClient{})

	cert, err := svc.InstallCustom(context.Background(), "ws-custom", "example.com", certPEM, keyPEM)
	if err != nil {
		t.Fatalf("InstallCustom() error = %v", err)
	}
	if cert.Issuer != "custom" || cert.AutoRenew {
		t.Fatalf("unexpected custom metadata: %#v", cert)
	}
	if !cert.ExpiresAt.Equal(wantNotAfter) {
		t.Fatalf("expires_at = %v, want %v", cert.ExpiresAt, wantNotAfter)
	}
}

func TestInstallCustomRejectsUnregisteredDomainBeforeSystemWrites(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-owner", "example.com")

	now := time.Now().UTC().Truncate(time.Second)
	certPEM, keyPEM := testCertificate(t, []string{"other.example.com"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	var sudoCalls int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudoCalls++
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, &MockACMEClient{}, t.TempDir())

	if _, err := svc.InstallCustom(context.Background(), "ws-owner", "other.example.com", certPEM, keyPEM); err == nil {
		t.Fatal("InstallCustom() accepted an unregistered domain")
	}
	if sudoCalls != 0 {
		t.Fatalf("system calls = %d, want 0", sudoCalls)
	}
}

func TestInstallCustomRejectsSuspendedWebsiteBeforeSystemWrites(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-suspended", "example.com")
	if _, err := db.Exec(`UPDATE websites SET status = 'suspended' WHERE id = 'ws-suspended'`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	certPEM, keyPEM := testCertificate(t, []string{"example.com"}, now.Add(-time.Hour), now.Add(24*time.Hour))
	var sudoCalls int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudoCalls++
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, &MockACMEClient{}, t.TempDir())
	if _, err := svc.InstallCustom(context.Background(), "ws-suspended", "example.com", certPEM, keyPEM); err == nil {
		t.Fatal("InstallCustom() accepted a suspended website")
	}
	if sudoCalls != 0 {
		t.Fatalf("system calls = %d, want 0", sudoCalls)
	}
}

func TestRevokeLetsEncryptStopsWhenCertificateCannotBeRead(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-revoke-read", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
		 VALUES ('cert-revoke-read', 'ws-revoke-read', 'example.com', 'letsencrypt', 'active', 1, ?, ?)`, now, now,
	); err != nil {
		t.Fatal(err)
	}
	var revokeCalls int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "cat" {
				return &executor.Result{ExitCode: 1, Stderr: "permission denied"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, &MockACMEClient{RevokeFunc: func(certPEM []byte) error {
		revokeCalls++
		return nil
	}}, t.TempDir())
	if err := svc.Revoke(context.Background(), "cert-revoke-read"); err == nil {
		t.Fatal("Revoke() succeeded without reading the certificate")
	}
	if revokeCalls != 0 {
		t.Fatalf("ACME revoke calls = %d, want 0", revokeCalls)
	}
	restored, err := svc.Get(context.Background(), "cert-revoke-read")
	if err != nil || restored.Status != "active" {
		t.Fatalf("certificate status = %q, err = %v; want active", restored.Status, err)
	}
}

func TestRevokeLetsEncryptNeverRestoresActiveStatusAfterRemoteRevocation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-revoke-remote", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
		 VALUES ('cert-revoke-remote', 'ws-revoke-remote', 'example.com', 'letsencrypt', 'active', 1, ?, ?)`, now, now,
	); err != nil {
		t.Fatal(err)
	}
	var nginxTests int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "cat":
				return &executor.Result{ExitCode: 0, Stdout: "certificate"}, nil
			case "test":
				return &executor.Result{ExitCode: 0}, nil
			case "nginx":
				nginxTests++
				if nginxTests == 1 {
					return &executor.Result{ExitCode: 1, Stderr: "removal failed"}, nil
				}
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, &MockACMEClient{RevokeFunc: func(certPEM []byte) error { return nil }}, t.TempDir())
	if err := svc.Revoke(context.Background(), "cert-revoke-remote"); err == nil {
		t.Fatal("Revoke() error = nil, want local removal failure")
	}
	cert, err := svc.Get(context.Background(), "cert-revoke-remote")
	if err != nil {
		t.Fatal(err)
	}
	if cert.Status != "revoked" {
		t.Fatalf("status = %q, want revoked after irreversible remote revocation", cert.Status)
	}

	if err := svc.Revoke(context.Background(), "cert-revoke-remote"); err != nil {
		t.Fatalf("Revoke() retry error = %v", err)
	}
}

func TestRevokeRetryDoesNotCallACMEAgain(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-revoke-retry", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
		 VALUES ('cert-revoke-retry', 'ws-revoke-retry', 'example.com', 'letsencrypt', 'revoked', 1, ?, ?)`, now, now,
	); err != nil {
		t.Fatal(err)
	}
	var revokeCalls int
	svc := NewService(db, newMockExecutor(), nil, &MockACMEClient{RevokeFunc: func(certPEM []byte) error {
		revokeCalls++
		return nil
	}}, t.TempDir())
	if err := svc.Revoke(context.Background(), "cert-revoke-retry"); err != nil {
		t.Fatalf("Revoke() retry error = %v", err)
	}
	if revokeCalls != 0 {
		t.Fatalf("ACME revoke calls = %d, want 0", revokeCalls)
	}
}

func TestRevokeFinishesCleanupAfterRequestCancellationAtCA(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-revoke-cancel", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
		 VALUES ('cert-revoke-cancel', 'ws-revoke-cancel', 'example.com', 'letsencrypt', 'active', 1, ?, ?)`, now, now,
	); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	mock := newMockExecutor()
	mock.RunSudoFunc = func(callCtx context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "cat" {
			return &executor.Result{ExitCode: 0, Stdout: "certificate"}, nil
		}
		if callCtx.Err() != nil {
			t.Fatalf("post-CA command %s received canceled context", name)
		}
		return &executor.Result{ExitCode: 0}, nil
	}
	svc := NewService(db, mock, nil, &MockACMEClient{RevokeFunc: func(certPEM []byte) error {
		cancel()
		return nil
	}}, t.TempDir())
	if err := svc.Revoke(ctx, "cert-revoke-cancel"); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	cert, err := svc.Get(context.Background(), "cert-revoke-cancel")
	if err != nil || cert.Status != "revoked" {
		t.Fatalf("certificate status = %q, err = %v; want revoked", cert.Status, err)
	}
}

func TestDeleteDuplicateCertificateKeepsSharedArtifacts(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-delete-duplicate", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"cert-duplicate-a", "cert-duplicate-b"} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-delete-duplicate', 'example.com', 'custom', 'active', 0, ?, ?)`, id, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE websites SET ssl_enabled = 1 WHERE id = 'ws-delete-duplicate'`); err != nil {
		t.Fatal(err)
	}
	var removedSharedArtifact bool
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "rm" {
				removedSharedArtifact = true
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, &MockACMEClient{}, t.TempDir())
	if err := svc.Delete(context.Background(), "cert-duplicate-a"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if removedSharedArtifact {
		t.Fatal("Delete() removed shared TLS artifacts while another active row exists")
	}
	var remainingID string
	if err := db.QueryRow(`SELECT id FROM ssl_certificates WHERE website_id = 'ws-delete-duplicate'`).Scan(&remainingID); err != nil {
		t.Fatal(err)
	}
	if remainingID != "cert-duplicate-b" {
		t.Fatalf("remaining certificate = %q, want canonical cert-duplicate-b", remainingID)
	}
	var sslEnabled int
	if err := db.QueryRow(`SELECT ssl_enabled FROM websites WHERE id = 'ws-delete-duplicate'`).Scan(&sslEnabled); err != nil {
		t.Fatal(err)
	}
	if sslEnabled != 1 {
		t.Fatalf("ssl_enabled = %d, want 1", sslEnabled)
	}
}

func TestDeleteCurrentCertificateRejectsHistoricalDuplicates(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-delete-current", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"cert-current-a", "cert-current-b"} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-delete-current', 'example.com', 'custom', 'active', 0, ?, ?)`, id, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}
	var sudoCalls int
	mock := newMockExecutor()
	mock.RunSudoFunc = func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		sudoCalls++
		return &executor.Result{ExitCode: 0}, nil
	}
	svc := NewService(db, mock, nil, &MockACMEClient{}, t.TempDir())
	if err := svc.Delete(context.Background(), "cert-current-b"); err == nil {
		t.Fatal("Delete() error = nil, want current duplicate rejection")
	}
	if sudoCalls != 0 {
		t.Fatalf("system calls = %d, want 0", sudoCalls)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ssl_certificates WHERE website_id = 'ws-delete-current'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("certificate rows = %d, want 2", count)
	}
}

func TestRevokeDuplicateCertificateKeepsSharedArtifactsAndSkipsACME(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-revoke-duplicate", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"cert-duplicate-a", "cert-duplicate-b"} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-revoke-duplicate', 'example.com', 'letsencrypt', 'active', 1, ?, ?)`, id, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}
	var revokeCalls, sudoCalls int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			sudoCalls++
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, &MockACMEClient{RevokeFunc: func(certPEM []byte) error {
		revokeCalls++
		return nil
	}}, t.TempDir())
	if err := svc.Revoke(context.Background(), "cert-duplicate-a"); err == nil {
		t.Fatal("Revoke() error = nil, want historical duplicate rejection")
	}
	if revokeCalls != 0 || sudoCalls != 0 {
		t.Fatalf("duplicate revoke performed shared operations: ACME=%d sudo=%d", revokeCalls, sudoCalls)
	}
	cert, err := svc.Get(context.Background(), "cert-duplicate-a")
	if err != nil || cert.Status != "active" {
		t.Fatalf("historical certificate status = %q, err = %v; want active", cert.Status, err)
	}
}

func TestRenewRejectsHistoricalDuplicate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-renew-duplicate", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"cert-renew-a", "cert-renew-b"} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-renew-duplicate', 'example.com', 'letsencrypt', 'active', 1, ?, ?)`, id, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}
	var obtainCalls int
	svc := NewService(db, newMockExecutor(), nil, &MockACMEClient{ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
		obtainCalls++
		return nil, nil, nil
	}}, t.TempDir())
	if err := svc.Renew(context.Background(), "cert-renew-a"); err == nil {
		t.Fatal("Renew() error = nil, want historical duplicate rejection")
	}
	if obtainCalls != 0 {
		t.Fatalf("ACME obtain calls = %d, want 0", obtainCalls)
	}
}

func TestIssueAndSuspendShareWebsiteMutationLock(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	db.SetMaxOpenConns(1)
	insertTestWebsite(t, db, "ws-lock", "example.com")
	issuedAt := time.Now().UTC().Truncate(time.Second)
	certPEM, keyPEM := testCertificate(t, []string{"example.com"}, issuedAt.Add(-time.Hour), issuedAt.Add(30*24*time.Hour))

	acmeStarted := make(chan struct{})
	releaseACME := make(chan struct{})
	var sequenceMu sync.Mutex
	var sequence []string
	mock := newMockExecutor()
	mock.RunSudoFunc = func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
		if name == "ln" || name == "mv" {
			sequenceMu.Lock()
			sequence = append(sequence, name)
			sequenceMu.Unlock()
		}
		return &executor.Result{ExitCode: 0}, nil
	}
	sslSvc := NewService(db, mock, nil, &MockACMEClient{ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
		close(acmeStarted)
		<-releaseACME
		return certPEM, keyPEM, nil
	}}, t.TempDir())
	websiteSvc := websiteconfig.NewService(db, mock, nil)

	issueDone := make(chan error, 1)
	go func() {
		_, err := sslSvc.Issue(context.Background(), "ws-lock", "example.com")
		issueDone <- err
	}()
	<-acmeStarted
	suspendDone := make(chan error, 1)
	go func() { suspendDone <- websiteSvc.Suspend(context.Background(), "ws-lock") }()
	select {
	case err := <-suspendDone:
		t.Fatalf("Suspend() completed before issuance released the shared lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseACME)
	if err := <-issueDone; err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if err := <-suspendDone; err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}
	sequenceMu.Lock()
	defer sequenceMu.Unlock()
	seenMove := false
	for _, operation := range sequence {
		if operation == "mv" {
			seenMove = true
		}
		if operation == "ln" && seenMove {
			t.Fatalf("certificate activation re-enabled config after suspension: %v", sequence)
		}
	}
}

func TestGetExpiringCertsReturnsOnlyCanonicalDuplicate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-expiring-duplicate", "example.com")
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	expires := now.Add(24 * time.Hour).Format(time.RFC3339)
	for _, id := range []string{"cert-expiring-a", "cert-expiring-b"} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
			 VALUES (?, 'ws-expiring-duplicate', 'example.com', 'letsencrypt', 'active', ?, 1, ?, ?)`, id, expires, nowStr, nowStr,
		); err != nil {
			t.Fatal(err)
		}
	}
	svc := newTestService(t, db, &MockACMEClient{})
	certs, err := svc.GetExpiringCerts(context.Background(), 14)
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 || certs[0].ID != "cert-expiring-b" {
		t.Fatalf("expiring certificates = %#v, want canonical cert-expiring-b", certs)
	}
}

func TestSetAutoRenewRejectsCustom(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-renew", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, 'custom', 'active', ?, 0, ?, ?)`,
		"cert-custom", "ws-renew", "example.com", now, now, now,
	); err != nil {
		t.Fatal(err)
	}

	svc := newTestService(t, db, &MockACMEClient{})
	if err := svc.SetAutoRenew(context.Background(), "cert-custom", true); err == nil {
		t.Fatal("SetAutoRenew() accepted a custom certificate")
	}
}

func TestRenewCustomDoesNotCallACME(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-custom-renew", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, 'custom', 'active', ?, 0, ?, ?)`,
		"cert-custom-renew", "ws-custom-renew", "example.com", now, now, now,
	); err != nil {
		t.Fatal(err)
	}
	var obtainCalls int
	acme := &MockACMEClient{ObtainFunc: func(domain, webroot string) ([]byte, []byte, error) {
		obtainCalls++
		return nil, nil, nil
	}}
	svc := newTestService(t, db, acme)
	if err := svc.Renew(context.Background(), "cert-custom-renew"); err == nil {
		t.Fatal("Renew() accepted a custom certificate")
	}
	if obtainCalls != 0 {
		t.Fatalf("ACME obtain calls = %d, want 0", obtainCalls)
	}
}

func TestCustomReplacementRestoresActiveRecordOnNginxFailure(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-replace", "example.com")
	oldExpiry := time.Now().UTC().Truncate(time.Second).Add(10 * 24 * time.Hour)
	nowStr := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, 'custom', 'active', ?, 0, ?, ?)`,
		"cert-replace", "ws-replace", "example.com", oldExpiry.Format(time.RFC3339), nowStr, nowStr,
	); err != nil {
		t.Fatal(err)
	}

	var nginxTests int
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			if name == "test" {
				return &executor.Result{ExitCode: 0}, nil
			}
			if name == "nginx" {
				nginxTests++
				if nginxTests == 1 {
					return &executor.Result{ExitCode: 1, Stderr: "replacement invalid"}, nil
				}
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	svc := NewService(db, mock, nil, &MockACMEClient{}, t.TempDir())
	issuedAt := time.Now().UTC().Truncate(time.Second)
	certPEM, keyPEM := testCertificate(t, []string{"example.com"}, issuedAt.Add(-time.Hour), issuedAt.Add(30*24*time.Hour))

	if _, err := svc.InstallCustom(context.Background(), "ws-replace", "example.com", certPEM, keyPEM); err == nil {
		t.Fatal("InstallCustom() error = nil, want replacement failure")
	}
	restored, err := svc.Get(context.Background(), "cert-replace")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != "active" || !restored.ExpiresAt.Equal(oldExpiry) {
		t.Fatalf("record was not restored: %#v", restored)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ssl_certificates WHERE website_id = ? AND domain = ?`, "ws-replace", "example.com").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("certificate rows = %d, want 1", count)
	}
}

func TestRevokeCustomDoesNotCallACME(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-revoke", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, 'custom', 'active', ?, 0, ?, ?)`,
		"cert-custom", "ws-revoke", "example.com", now, now, now,
	); err != nil {
		t.Fatal(err)
	}

	var revoked bool
	acme := &MockACMEClient{RevokeFunc: func(certPEM []byte) error { revoked = true; return nil }}
	svc := newTestService(t, db, acme)
	if err := svc.Revoke(context.Background(), "cert-custom"); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if revoked {
		t.Fatal("custom certificate was sent to ACME revocation")
	}
}

func TestDeleteKeepsWebsiteSSLWhenAnotherCertificateIsActive(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-multi", "example.com")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO domains (id, website_id, name, type, created_at) VALUES (?, ?, ?, 'alias', ?)`,
		"dom-multi-alias", "ws-multi", "www.example.com", now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE websites SET ssl_enabled = 1 WHERE id = ?`, "ws-multi"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, domain string }{{"cert-primary", "example.com"}, {"cert-alias", "www.example.com"}} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
			 VALUES (?, ?, ?, 'letsencrypt', 'active', ?, 1, ?, ?)`,
			item.id, "ws-multi", item.domain, now, now, now,
		); err != nil {
			t.Fatal(err)
		}
	}

	svc := newTestService(t, db, &MockACMEClient{})
	if err := svc.Delete(context.Background(), "cert-alias"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	var enabled int
	if err := db.QueryRow(`SELECT ssl_enabled FROM websites WHERE id = ?`, "ws-multi").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("ssl_enabled = %d, want 1", enabled)
	}
}

func TestGetExpiringCertsExcludesCustomIssuer(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	insertTestWebsite(t, db, "ws-expiring-issuer", "example.com")
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	expires := now.Add(24 * time.Hour).Format(time.RFC3339)
	for _, item := range []struct{ id, issuer string }{{"cert-le", "letsencrypt"}, {"cert-custom", "custom"}} {
		if _, err := db.Exec(
			`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 'active', ?, 1, ?, ?)`,
			item.id, "ws-expiring-issuer", item.id+".example.com", item.issuer, expires, nowStr, nowStr,
		); err != nil {
			t.Fatal(err)
		}
	}
	svc := newTestService(t, db, &MockACMEClient{})
	certs, err := svc.GetExpiringCerts(context.Background(), 14)
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 || certs[0].ID != "cert-le" {
		t.Fatalf("expiring certificates = %#v, want only cert-le", certs)
	}
}

// acmeError is a simple error type for testing ACME failures.
type acmeError struct {
	msg string
}

func (e *acmeError) Error() string {
	return e.msg
}
