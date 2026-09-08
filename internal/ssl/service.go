package ssl

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	nginxconfig "github.com/mohammadirham37/jenderal_panel/internal/nginx"
	websiteconfig "github.com/mohammadirham37/jenderal_panel/internal/website"
)

// Service manages SSL certificate lifecycle: issuance, renewal, revocation and deletion.
type Service struct {
	db            *sql.DB
	exec          executor.CommandExecutor
	audit         *audit.Service
	acme          ACMEClient
	certDir       string
	ipv6Available func() bool
	mutationMu    sync.Mutex
}

// NewService creates a new SSL management service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service, acme ACMEClient, certDir string) *Service {
	return &Service{
		db:            db,
		exec:          exec,
		audit:         auditSvc,
		acme:          acme,
		certDir:       certDir,
		ipv6Available: nginxconfig.IPv6Available,
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
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return model.SSLCertificate{}, model.NewValidationError("domain is required")
	}
	if websiteID == "" {
		return model.SSLCertificate{}, model.NewValidationError("website_id is required")
	}

	site, err := s.loadSiteForDomain(ctx, websiteID, domain)
	if err != nil {
		return model.SSLCertificate{}, err
	}
	if site.Status != "active" {
		return model.SSLCertificate{}, model.NewValidationError("SSL certificates can only be installed on an active website")
	}
	if existing, found, err := s.findCertificateByDomain(ctx, websiteID, domain); err != nil {
		return model.SSLCertificate{}, err
	} else if found && (existing.Status == "active" || existing.Status == "pending" || existing.Status == "issuing") {
		return model.SSLCertificate{}, model.NewValidationError("an SSL certificate is already active or being installed for this domain")
	}

	now := time.Now().UTC()
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

	cert, err = s.savePendingCertificate(ctx, cert)
	if err != nil {
		return model.SSLCertificate{}, err
	}

	certPEM, keyPEM, err := s.acme.ObtainCertificate(domain, websiteconfig.DefaultACMEChallengeRoot)
	if err != nil {
		msg := fmt.Sprintf("obtain certificate: %v", err)
		s.markFailed(ctx, cert.ID, msg)
		cert.Status = "failed"
		cert.ErrorMessage = msg
		return cert, nil
	}

	metadata, err := validateCertificateMaterial(certPEM, keyPEM, domain, time.Now().UTC())
	if err != nil {
		msg := err.Error()
		s.markFailed(ctx, cert.ID, msg)
		cert.Status = "failed"
		cert.ErrorMessage = msg
		return cert, nil
	}

	redirectDomains, err := s.activeDomains(ctx, websiteID, domain)
	if err != nil {
		return model.SSLCertificate{}, err
	}
	activation, err := s.beginCertificateActivation(ctx, activationRequest{Site: site, CertificatePEM: certPEM, PrivateKeyPEM: keyPEM, RedirectDomains: redirectDomains})
	if err != nil {
		msg := fmt.Sprintf("enable HTTPS: %v", err)
		s.markFailed(ctx, cert.ID, msg)
		cert.Status = "failed"
		cert.ErrorMessage = msg
		return cert, nil
	}

	updatedAt := time.Now().UTC()
	if err := s.commitActiveCertificate(ctx, cert.ID, websiteID, "letsencrypt", metadata.NotAfter, true, updatedAt); err != nil {
		rollbackErr := activation.rollback()
		s.markFailed(ctx, cert.ID, err.Error())
		if rollbackErr != nil {
			return model.SSLCertificate{}, fmt.Errorf("%v; rollback failed: %w", err, rollbackErr)
		}
		return model.SSLCertificate{}, err
	}
	activation.commit()

	cert.Status = "active"
	cert.ExpiresAt = metadata.NotAfter
	cert.UpdatedAt = updatedAt

	return cert, nil
}

// Renew renews an existing certificate by re-obtaining it from the ACME CA.
func (s *Service) Renew(ctx context.Context, certID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}
	if cert.Issuer != "letsencrypt" {
		return model.NewValidationError("only Let's Encrypt certificates can be renewed automatically")
	}

	site, err := s.loadSiteForDomain(ctx, cert.WebsiteID, cert.Domain)
	if err != nil {
		return err
	}
	if site.Status != "active" {
		return model.NewValidationError("SSL certificates can only be renewed on an active website")
	}

	certPEM, keyPEM, err := s.acme.ObtainCertificate(cert.Domain, websiteconfig.DefaultACMEChallengeRoot)
	if err != nil {
		return fmt.Errorf("renew certificate: %w", err)
	}
	metadata, err := validateCertificateMaterial(certPEM, keyPEM, cert.Domain, time.Now().UTC())
	if err != nil {
		return err
	}
	redirectDomains, err := s.activeDomains(ctx, cert.WebsiteID, cert.Domain)
	if err != nil {
		return err
	}
	activation, err := s.beginCertificateActivation(ctx, activationRequest{Site: site, CertificatePEM: certPEM, PrivateKeyPEM: keyPEM, RedirectDomains: redirectDomains})
	if err != nil {
		return err
	}

	updatedAt := time.Now().UTC()
	if err := s.commitActiveCertificate(ctx, cert.ID, cert.WebsiteID, cert.Issuer, metadata.NotAfter, cert.AutoRenew, updatedAt); err != nil {
		if rollbackErr := activation.rollback(); rollbackErr != nil {
			return fmt.Errorf("%v; rollback failed: %w", err, rollbackErr)
		}
		return err
	}
	activation.commit()

	return nil
}

// Revoke revokes a certificate, removes cert files, reverts nginx to HTTP-only,
// and updates the database status to revoked.
func (s *Service) Revoke(ctx context.Context, certID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}
	updatedStr := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET status = 'revoking', updated_at = ? WHERE id = ?`,
		updatedStr, certID,
	); err != nil {
		return fmt.Errorf("prepare certificate revocation: %w", err)
	}

	remotelyRevoked := false
	if cert.Issuer == "letsencrypt" {
		certPath := filepath.Join(s.certDir, cert.Domain, "cert.pem")
		readResult, readErr := s.exec.RunSudo(ctx, "cat", certPath)
		if readErr != nil {
			_ = s.restoreCertificateRecord(ctx, cert)
			return fmt.Errorf("read certificate for revocation: %w", readErr)
		}
		if readResult.ExitCode != 0 {
			_ = s.restoreCertificateRecord(ctx, cert)
			return fmt.Errorf("read certificate for revocation: %s", strings.TrimSpace(readResult.Stderr))
		}
		if revokeErr := s.acme.RevokeCertificate([]byte(readResult.Stdout)); revokeErr != nil {
			_ = s.restoreCertificateRecord(ctx, cert)
			return fmt.Errorf("revoke certificate: %w", revokeErr)
		}
		remotelyRevoked = true
		if _, err := s.db.ExecContext(ctx,
			`UPDATE ssl_certificates SET status = 'revoked', updated_at = ? WHERE id = ?`,
			updatedStr, certID,
		); err != nil {
			return fmt.Errorf("record remote certificate revocation: %w", err)
		}
	}

	site, err := s.loadSiteForDomain(ctx, cert.WebsiteID, cert.Domain)
	if err != nil {
		if !remotelyRevoked {
			_ = s.restoreCertificateRecord(ctx, cert)
		}
		return err
	}
	remaining, err := s.activeDomains(ctx, cert.WebsiteID, "")
	if err != nil {
		if !remotelyRevoked {
			_ = s.restoreCertificateRecord(ctx, cert)
		}
		return err
	}
	if err := s.removeCertificateConfig(ctx, site, remaining); err != nil {
		if !remotelyRevoked {
			_ = s.restoreCertificateRecord(ctx, cert)
		}
		return err
	}
	if !remotelyRevoked {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE ssl_certificates SET status = 'revoked', updated_at = ? WHERE id = ?`,
			updatedStr, certID,
		); err != nil {
			return fmt.Errorf("update ssl certificate: %w", err)
		}
	}
	if err := s.setWebsiteSSLState(ctx, cert.WebsiteID); err != nil {
		return err
	}

	return nil
}

// Delete removes a certificate entirely: removes files, reverts nginx, and
// deletes the database record.
func (s *Service) Delete(ctx context.Context, certID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `UPDATE ssl_certificates SET status = 'deleting' WHERE id = ?`, certID)
	if err != nil {
		return fmt.Errorf("prepare ssl certificate deletion: %w", err)
	}
	site, err := s.loadSiteForDomain(ctx, cert.WebsiteID, cert.Domain)
	if err != nil {
		_ = s.restoreCertificateRecord(ctx, cert)
		return err
	}
	remaining, err := s.activeDomains(ctx, cert.WebsiteID, "")
	if err != nil {
		_ = s.restoreCertificateRecord(ctx, cert)
		return err
	}
	if err := s.removeCertificateConfig(ctx, site, remaining); err != nil {
		_ = s.restoreCertificateRecord(ctx, cert)
		return err
	}
	if _, err = s.db.ExecContext(ctx, `DELETE FROM ssl_certificates WHERE id = ?`, certID); err != nil {
		return fmt.Errorf("delete ssl certificate: %w", err)
	}
	if err := s.setWebsiteSSLState(ctx, cert.WebsiteID); err != nil {
		return err
	}

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
		 WHERE issuer = 'letsencrypt' AND auto_renew = 1 AND status = 'active' AND expires_at <= ?
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
	cleanupCtx, cancel := cleanupContext()
	defer cancel()
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(cleanupCtx,
		`UPDATE ssl_certificates SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`,
		"failed", errMsg, now, certID,
	)
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
