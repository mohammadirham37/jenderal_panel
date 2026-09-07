package ssl

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Service manages SSL certificate lifecycle: issuance, renewal, revocation and deletion.
type Service struct {
	db      *sql.DB
	exec    executor.CommandExecutor
	audit   *audit.Service
	acme    ACMEClient
	certDir string
}

// NewService creates a new SSL management service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service, acme ACMEClient, certDir string) *Service {
	return &Service{
		db:      db,
		exec:    exec,
		audit:   auditSvc,
		acme:    acme,
		certDir: certDir,
	}
}

// Issue requests and installs a new SSL certificate for the given domain.
//
// Flow:
//  1. Insert a DB record with status=pending.
//  2. Create the certificate directory via RunSudo.
//  3. Call ACMEClient.ObtainCertificate to get cert and key PEM bytes.
//  4. Write cert.pem and key.pem to certDir/{domain}/.
//  5. Generate an HTTPS nginx vhost, validate it, and reload nginx.
//  6. Update the DB record to status=active with the certificate expiry.
//
// On failure the record is updated to status=failed with an error message.
func (s *Service) Issue(ctx context.Context, websiteID, domain string) (model.SSLCertificate, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return model.SSLCertificate{}, model.NewValidationError("domain is required")
	}
	if websiteID == "" {
		return model.SSLCertificate{}, model.NewValidationError("website_id is required")
	}

	// Look up the website's document root.
	docRoot, err := s.getWebsiteDocRoot(ctx, websiteID)
	if err != nil {
		return model.SSLCertificate{}, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	certID := ulid.Make().String()

	cert := model.SSLCertificate{
		ID:        certID,
		WebsiteID: websiteID,
		Domain:    domain,
		Issuer:    "letsencrypt",
		Status:    "pending",
		AutoRenew: true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// 1. Insert pending record.
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		cert.ID, cert.WebsiteID, cert.Domain, cert.Issuer, cert.Status,
		boolToInt(cert.AutoRenew), nowStr, nowStr,
	)
	if err != nil {
		return model.SSLCertificate{}, fmt.Errorf("insert ssl certificate: %w", err)
	}

	// 2. Create cert directory.
	domainDir := filepath.Join(s.certDir, domain)
	result, err := s.exec.RunSudo(ctx, "mkdir", "-p", domainDir)
	if err != nil {
		s.markFailed(ctx, certID, fmt.Sprintf("create cert dir: %v", err))
		cert.Status = "failed"
		cert.ErrorMessage = fmt.Sprintf("create cert dir: %v", err)
		return cert, nil
	}
	if result.ExitCode != 0 {
		msg := fmt.Sprintf("create cert dir: %s", strings.TrimSpace(result.Stderr))
		s.markFailed(ctx, certID, msg)
		cert.Status = "failed"
		cert.ErrorMessage = msg
		return cert, nil
	}

	// 3. Obtain certificate from ACME.
	certPEM, keyPEM, err := s.acme.ObtainCertificate(domain, docRoot)
	if err != nil {
		msg := fmt.Sprintf("obtain certificate: %v", err)
		s.markFailed(ctx, certID, msg)
		cert.Status = "failed"
		cert.ErrorMessage = msg
		return cert, nil
	}

	// 4. Write cert and key files.
	if err := s.writeCertFiles(ctx, domainDir, certPEM, keyPEM); err != nil {
		msg := fmt.Sprintf("write cert files: %v", err)
		s.markFailed(ctx, certID, msg)
		cert.Status = "failed"
		cert.ErrorMessage = msg
		return cert, nil
	}

	// 5. Update nginx to HTTPS and reload.
	if err := s.enableHTTPS(ctx, domain, domainDir); err != nil {
		msg := fmt.Sprintf("enable HTTPS: %v", err)
		s.markFailed(ctx, certID, msg)
		cert.Status = "failed"
		cert.ErrorMessage = msg
		return cert, nil
	}

	// 6. Mark active.
	expiresAt := now.Add(90 * 24 * time.Hour) // Let's Encrypt certs are valid for ~90 days.
	expiresStr := expiresAt.Format(time.RFC3339)
	updatedStr := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET status = ?, expires_at = ?, updated_at = ? WHERE id = ?`,
		"active", expiresStr, updatedStr, certID,
	)
	if err != nil {
		return model.SSLCertificate{}, fmt.Errorf("update ssl certificate: %w", err)
	}

	// Update website ssl_enabled flag.
	_, _ = s.db.ExecContext(ctx,
		`UPDATE websites SET ssl_enabled = 1, updated_at = ? WHERE id = ?`,
		updatedStr, websiteID,
	)

	cert.Status = "active"
	cert.ExpiresAt = expiresAt
	cert.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return cert, nil
}

// Renew renews an existing certificate by re-obtaining it from the ACME CA.
func (s *Service) Renew(ctx context.Context, certID string) error {
	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}

	docRoot, err := s.getWebsiteDocRoot(ctx, cert.WebsiteID)
	if err != nil {
		return err
	}

	domainDir := filepath.Join(s.certDir, cert.Domain)

	certPEM, keyPEM, err := s.acme.ObtainCertificate(cert.Domain, docRoot)
	if err != nil {
		msg := fmt.Sprintf("renew certificate: %v", err)
		s.markFailed(ctx, certID, msg)
		return fmt.Errorf("renew certificate: %w", err)
	}

	if err := s.writeCertFiles(ctx, domainDir, certPEM, keyPEM); err != nil {
		msg := fmt.Sprintf("write cert files: %v", err)
		s.markFailed(ctx, certID, msg)
		return fmt.Errorf("write cert files: %w", err)
	}

	// Reload nginx to pick up new cert.
	_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")

	expiresAt := time.Now().UTC().Add(90 * 24 * time.Hour)
	expiresStr := expiresAt.Format(time.RFC3339)
	updatedStr := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET status = ?, expires_at = ?, error_message = NULL, updated_at = ? WHERE id = ?`,
		"active", expiresStr, updatedStr, certID,
	)
	if err != nil {
		return fmt.Errorf("update ssl certificate: %w", err)
	}

	return nil
}

// Revoke revokes a certificate, removes cert files, reverts nginx to HTTP-only,
// and updates the database status to revoked.
func (s *Service) Revoke(ctx context.Context, certID string) error {
	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}

	domainDir := filepath.Join(s.certDir, cert.Domain)
	certPath := filepath.Join(domainDir, "cert.pem")

	// Read the certificate file to pass to ACME revocation.
	readResult, err := s.exec.RunSudo(ctx, "cat", certPath)
	if err == nil && readResult.ExitCode == 0 {
		if revokeErr := s.acme.RevokeCertificate([]byte(readResult.Stdout)); revokeErr != nil {
			return fmt.Errorf("revoke certificate: %w", revokeErr)
		}
	}

	// Remove cert files.
	_, _ = s.exec.RunSudo(ctx, "rm", "-rf", domainDir)

	// Revert nginx to HTTP-only.
	s.disableHTTPS(ctx, cert.Domain)

	// Update DB.
	updatedStr := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET status = ?, updated_at = ? WHERE id = ?`,
		"revoked", updatedStr, certID,
	)
	if err != nil {
		return fmt.Errorf("update ssl certificate: %w", err)
	}

	// Clear website ssl_enabled flag.
	_, _ = s.db.ExecContext(ctx,
		`UPDATE websites SET ssl_enabled = 0, updated_at = ? WHERE id = ?`,
		updatedStr, cert.WebsiteID,
	)

	return nil
}

// Delete removes a certificate entirely: removes files, reverts nginx, and
// deletes the database record.
func (s *Service) Delete(ctx context.Context, certID string) error {
	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}

	domainDir := filepath.Join(s.certDir, cert.Domain)

	// Remove cert files.
	_, _ = s.exec.RunSudo(ctx, "rm", "-rf", domainDir)

	// Revert nginx to HTTP-only.
	s.disableHTTPS(ctx, cert.Domain)

	// Delete DB record.
	_, err = s.db.ExecContext(ctx, `DELETE FROM ssl_certificates WHERE id = ?`, certID)
	if err != nil {
		return fmt.Errorf("delete ssl certificate: %w", err)
	}

	// Clear website ssl_enabled flag.
	updatedStr := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE websites SET ssl_enabled = 0, updated_at = ? WHERE id = ?`,
		updatedStr, cert.WebsiteID,
	)

	return nil
}

// Get returns a single SSL certificate by ID.
func (s *Service) Get(ctx context.Context, certID string) (model.SSLCertificate, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, website_id, domain, issuer, status, expires_at, auto_renew, error_message, created_at, updated_at
		 FROM ssl_certificates WHERE id = ?`, certID)

	cert, err := scanCert(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.SSLCertificate{}, model.ErrNotFound
		}
		return model.SSLCertificate{}, fmt.Errorf("get ssl certificate: %w", err)
	}
	return cert, nil
}

// List returns all SSL certificates ordered by creation time descending.
func (s *Service) List(ctx context.Context) ([]model.SSLCertificate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, domain, issuer, status, expires_at, auto_renew, error_message, created_at, updated_at
		 FROM ssl_certificates ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list ssl certificates: %w", err)
	}
	defer rows.Close()

	var certs []model.SSLCertificate
	for rows.Next() {
		cert, err := scanCertRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ssl certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	return certs, rows.Err()
}

// ListByWebsite returns all SSL certificates for a specific website.
func (s *Service) ListByWebsite(ctx context.Context, websiteID string) ([]model.SSLCertificate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, domain, issuer, status, expires_at, auto_renew, error_message, created_at, updated_at
		 FROM ssl_certificates WHERE website_id = ? ORDER BY created_at DESC`, websiteID)
	if err != nil {
		return nil, fmt.Errorf("list ssl certificates by website: %w", err)
	}
	defer rows.Close()

	var certs []model.SSLCertificate
	for rows.Next() {
		cert, err := scanCertRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ssl certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	return certs, rows.Err()
}

// Count returns the total number of SSL certificates.
func (s *Service) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ssl_certificates`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count ssl certificates: %w", err)
	}
	return count, nil
}

// GetExpiringCerts returns certificates that expire within the given number of
// days and have auto_renew enabled.
func (s *Service) GetExpiringCerts(ctx context.Context, days int) ([]model.SSLCertificate, error) {
	cutoff := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, domain, issuer, status, expires_at, auto_renew, error_message, created_at, updated_at
		 FROM ssl_certificates
		 WHERE auto_renew = 1 AND status = 'active' AND expires_at <= ?
		 ORDER BY expires_at ASC`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("get expiring certificates: %w", err)
	}
	defer rows.Close()

	var certs []model.SSLCertificate
	for rows.Next() {
		cert, err := scanCertRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ssl certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	return certs, rows.Err()
}

// ----------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------

// markFailed updates a certificate record to status=failed with the given error message.
func (s *Service) markFailed(ctx context.Context, certID, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`,
		"failed", errMsg, now, certID,
	)
}

// getWebsiteDocRoot queries the website table for the document_root of the given website ID.
func (s *Service) getWebsiteDocRoot(ctx context.Context, websiteID string) (string, error) {
	var docRoot string
	err := s.db.QueryRowContext(ctx,
		`SELECT document_root FROM websites WHERE id = ?`, websiteID,
	).Scan(&docRoot)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", model.ErrNotFound
		}
		return "", fmt.Errorf("get website document root: %w", err)
	}
	return docRoot, nil
}

// writeCertFiles writes the certificate and key PEM data to the given directory
// using sudo so that root-owned directories are accessible.
func (s *Service) writeCertFiles(ctx context.Context, domainDir string, certPEM, keyPEM []byte) error {
	certPath := filepath.Join(domainDir, "cert.pem")
	keyPath := filepath.Join(domainDir, "key.pem")

	// Write to temp files first, then move with sudo.
	certTmp := "/tmp/jenderal_ssl_cert.tmp"
	keyTmp := "/tmp/jenderal_ssl_key.tmp"

	if err := os.WriteFile(certTmp, certPEM, 0600); err != nil {
		return fmt.Errorf("write temp cert: %w", err)
	}
	if err := os.WriteFile(keyTmp, keyPEM, 0600); err != nil {
		return fmt.Errorf("write temp key: %w", err)
	}

	// Copy cert.
	result, err := s.exec.RunSudo(ctx, "cp", certTmp, certPath)
	if err != nil {
		return fmt.Errorf("copy cert: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("copy cert: %s", strings.TrimSpace(result.Stderr))
	}

	// Copy key.
	result, err = s.exec.RunSudo(ctx, "cp", keyTmp, keyPath)
	if err != nil {
		return fmt.Errorf("copy key: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("copy key: %s", strings.TrimSpace(result.Stderr))
	}

	// Set secure permissions on the key.
	_, _ = s.exec.RunSudo(ctx, "chmod", "600", keyPath)
	_, _ = s.exec.RunSudo(ctx, "chmod", "644", certPath)

	return nil
}

// enableHTTPS generates an HTTPS nginx snippet, writes it to the site config,
// validates nginx, and reloads.
func (s *Service) enableHTTPS(ctx context.Context, domain, domainDir string) error {
	certPath := filepath.Join(domainDir, "cert.pem")
	keyPath := filepath.Join(domainDir, "key.pem")

	// Build the SSL snippet to append to the nginx config.
	sslSnippet := fmt.Sprintf(`
# SSL configuration managed by Jenderal Panel
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;

    server_name %s;

    ssl_certificate %s;
    ssl_certificate_key %s;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;

    include /etc/nginx/sites-available/%s;
}
`, domain, certPath, keyPath, domain+".location")

	// Write SSL config.
	sslConfPath := "/etc/nginx/sites-available/" + domain + ".ssl"
	tmpPath := "/tmp/jenderal_ssl_vhost.tmp"

	if err := os.WriteFile(tmpPath, []byte(sslSnippet), 0644); err != nil {
		return fmt.Errorf("write temp ssl config: %w", err)
	}

	result, err := s.exec.RunSudo(ctx, "cp", tmpPath, sslConfPath)
	if err != nil {
		return fmt.Errorf("write ssl config: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write ssl config: %s", strings.TrimSpace(result.Stderr))
	}

	// Enable the SSL config.
	enabledPath := "/etc/nginx/sites-enabled/" + domain + ".ssl"
	_, _ = s.exec.RunSudo(ctx, "ln", "-sf", sslConfPath, enabledPath)

	// Validate nginx configuration.
	testResult, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return fmt.Errorf("test nginx config: %w", err)
	}
	if testResult.ExitCode != 0 {
		// Roll back: remove the SSL config.
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", sslConfPath)
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", enabledPath)
		return fmt.Errorf("nginx config test failed: %s", strings.TrimSpace(testResult.Stderr))
	}

	// Reload nginx.
	_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")

	return nil
}

// disableHTTPS removes the SSL nginx config and reloads nginx.
func (s *Service) disableHTTPS(ctx context.Context, domain string) {
	sslConfPath := "/etc/nginx/sites-available/" + domain + ".ssl"
	enabledPath := "/etc/nginx/sites-enabled/" + domain + ".ssl"

	_, _ = s.exec.RunSudo(ctx, "rm", "-f", sslConfPath)
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", enabledPath)
	_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")
}

// scanCert scans a single SSL certificate row from *sql.Row.
func scanCert(row *sql.Row) (model.SSLCertificate, error) {
	var cert model.SSLCertificate
	var expiresAt, errorMessage sql.NullString
	var autoRenew int
	var createdStr, updatedStr string

	err := row.Scan(
		&cert.ID, &cert.WebsiteID, &cert.Domain, &cert.Issuer,
		&cert.Status, &expiresAt, &autoRenew, &errorMessage,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return cert, err
	}

	cert.AutoRenew = autoRenew == 1
	cert.ErrorMessage = errorMessage.String
	if expiresAt.Valid {
		cert.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt.String)
	}
	cert.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	cert.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return cert, nil
}

// scanCertRows scans a single SSL certificate row from *sql.Rows.
func scanCertRows(rows *sql.Rows) (model.SSLCertificate, error) {
	var cert model.SSLCertificate
	var expiresAt, errorMessage sql.NullString
	var autoRenew int
	var createdStr, updatedStr string

	err := rows.Scan(
		&cert.ID, &cert.WebsiteID, &cert.Domain, &cert.Issuer,
		&cert.Status, &expiresAt, &autoRenew, &errorMessage,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return cert, err
	}

	cert.AutoRenew = autoRenew == 1
	cert.ErrorMessage = errorMessage.String
	if expiresAt.Valid {
		cert.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt.String)
	}
	cert.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	cert.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return cert, nil
}

// boolToInt converts a boolean to an integer for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullableString returns nil if s is empty, otherwise returns s.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
