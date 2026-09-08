package ssl

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// InstallCustom validates and installs operator-provided certificate material.
func (s *Service) InstallCustom(ctx context.Context, websiteID, domain string, certPEM, keyPEM []byte) (model.SSLCertificate, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if websiteID == "" {
		return model.SSLCertificate{}, model.NewValidationError("website_id is required")
	}
	unlock := s.mutations.Lock(websiteID)
	defer unlock()
	site, err := s.loadSiteForDomain(ctx, websiteID, domain)
	if err != nil {
		return model.SSLCertificate{}, err
	}
	if site.Status != "active" {
		return model.SSLCertificate{}, model.NewValidationError("SSL certificates can only be installed on an active website")
	}
	metadata, err := validateCertificateMaterial(certPEM, keyPEM, domain, time.Now().UTC())
	if err != nil {
		return model.SSLCertificate{}, err
	}

	existing, found, err := s.findCertificateByDomain(ctx, websiteID, domain)
	if err != nil {
		return model.SSLCertificate{}, err
	}
	now := time.Now().UTC()
	cert := model.SSLCertificate{
		ID: ulid.Make().String(), WebsiteID: websiteID, Domain: domain,
		Issuer: "custom", Status: "issuing", ExpiresAt: metadata.NotAfter,
		AutoRenew: false, CreatedAt: now, UpdatedAt: now,
	}
	cert, err = s.savePendingCertificate(ctx, cert)
	if err != nil {
		return model.SSLCertificate{}, err
	}

	site, err = s.loadSiteForDomain(ctx, websiteID, domain)
	if err != nil {
		return model.SSLCertificate{}, s.recoverPendingFailure(cert.ID, err, existing, found)
	}
	if site.Status != "active" {
		return model.SSLCertificate{}, s.recoverPendingFailure(cert.ID, model.NewValidationError("SSL certificates can only be installed on an active website"), existing, found)
	}
	redirectDomains, err := s.activeDomains(ctx, websiteID, domain)
	if err != nil {
		return model.SSLCertificate{}, s.recoverPendingFailure(cert.ID, err, existing, found)
	}
	activation, err := s.beginCertificateActivation(ctx, activationRequest{Site: site, CertificatePEM: certPEM, PrivateKeyPEM: keyPEM, RedirectDomains: redirectDomains})
	if err != nil {
		message := fmt.Sprintf("install custom certificate: %v", err)
		if recoveryErr := s.recoverPendingCertificate(cert.ID, message, existing, found); recoveryErr != nil {
			return model.SSLCertificate{}, fmt.Errorf("%s; recover certificate record: %w", message, recoveryErr)
		}
		if found {
			return model.SSLCertificate{}, fmt.Errorf("install custom certificate: %w", err)
		}
		cert.Status = "failed"
		cert.ErrorMessage = message
		return cert, nil
	}

	updated := time.Now().UTC()
	if err := s.commitActiveCertificate(ctx, cert.ID, websiteID, "custom", metadata.NotAfter, false, updated); err != nil {
		rollbackErr := activation.rollback()
		recoveryErr := s.recoverPendingCertificate(cert.ID, err.Error(), existing, found)
		if rollbackErr != nil {
			return model.SSLCertificate{}, fmt.Errorf("%v; rollback failed: %w", err, rollbackErr)
		}
		if recoveryErr != nil {
			return model.SSLCertificate{}, fmt.Errorf("%v; recover certificate record: %w", err, recoveryErr)
		}
		return model.SSLCertificate{}, err
	}
	activation.commit()
	cert.Status = "active"
	cert.ExpiresAt = metadata.NotAfter
	cert.UpdatedAt = updated
	return cert, nil
}

// SetAutoRenew changes renewal policy for a Let's Encrypt certificate.
func (s *Service) SetAutoRenew(ctx context.Context, certID string, enabled bool) error {
	cert, err := s.Get(ctx, certID)
	if err != nil {
		return err
	}
	unlock := s.mutations.Lock(cert.WebsiteID)
	defer unlock()
	cert, err = s.Get(ctx, certID)
	if err != nil {
		return err
	}
	if cert.Issuer != "letsencrypt" {
		return model.NewValidationError("auto-renew is only available for Let's Encrypt certificates")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE ssl_certificates SET auto_renew = ?, updated_at = ? WHERE id = ?`,
		boolToInt(enabled), time.Now().UTC().Format(time.RFC3339), certID,
	)
	if err != nil {
		return fmt.Errorf("update auto-renew: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (s *Service) findCertificateByDomain(ctx context.Context, websiteID, domain string) (model.SSLCertificate, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, website_id, domain, issuer, status, expires_at, auto_renew, error_message, created_at, updated_at
		 FROM ssl_certificates WHERE website_id = ? AND domain = ? ORDER BY created_at DESC, id DESC LIMIT 1`,
		websiteID, domain,
	)
	cert, err := scanCert(row)
	if err == sql.ErrNoRows {
		return model.SSLCertificate{}, false, nil
	}
	if err != nil {
		return model.SSLCertificate{}, false, fmt.Errorf("find domain certificate: %w", err)
	}
	return cert, true, nil
}

func (s *Service) savePendingCertificate(ctx context.Context, cert model.SSLCertificate) (model.SSLCertificate, error) {
	existing, found, err := s.findCertificateByDomain(ctx, cert.WebsiteID, cert.Domain)
	if err != nil {
		return model.SSLCertificate{}, err
	}
	if found {
		cert.ID = existing.ID
		cert.CreatedAt = existing.CreatedAt
		_, err = s.db.ExecContext(ctx,
			`UPDATE ssl_certificates SET issuer = ?, status = ?, expires_at = ?, auto_renew = ?, error_message = NULL, updated_at = ? WHERE id = ?`,
			cert.Issuer, cert.Status, nullableTime(cert.ExpiresAt), boolToInt(cert.AutoRenew), cert.UpdatedAt.Format(time.RFC3339), cert.ID,
		)
		if err != nil {
			return model.SSLCertificate{}, fmt.Errorf("update pending certificate: %w", err)
		}
		return cert, nil
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO ssl_certificates (id, website_id, domain, issuer, status, expires_at, auto_renew, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cert.ID, cert.WebsiteID, cert.Domain, cert.Issuer, cert.Status,
		nullableTime(cert.ExpiresAt), boolToInt(cert.AutoRenew), cert.CreatedAt.Format(time.RFC3339), cert.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return model.SSLCertificate{}, fmt.Errorf("insert ssl certificate: %w", err)
	}
	return cert, nil
}

func (s *Service) restoreCertificateRecord(ctx context.Context, cert model.SSLCertificate) error {
	cleanupCtx, cancel := cleanupContext()
	defer cancel()
	_, err := s.db.ExecContext(cleanupCtx,
		`UPDATE ssl_certificates SET issuer = ?, status = ?, expires_at = ?, auto_renew = ?, error_message = ?, updated_at = ? WHERE id = ?`,
		cert.Issuer, cert.Status, nullableTime(cert.ExpiresAt), boolToInt(cert.AutoRenew), nullableString(cert.ErrorMessage), cert.UpdatedAt.Format(time.RFC3339), cert.ID,
	)
	return err
}

func (s *Service) activeDomains(ctx context.Context, websiteID, includeDomain string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT domain FROM ssl_certificates WHERE website_id = ? AND status = 'active' ORDER BY created_at ASC`,
		websiteID,
	)
	if err != nil {
		return nil, fmt.Errorf("list active SSL domains: %w", err)
	}
	defer rows.Close()
	seen := make(map[string]struct{})
	var domains []string
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, fmt.Errorf("scan active SSL domain: %w", err)
		}
		if _, exists := seen[domain]; !exists {
			seen[domain] = struct{}{}
			domains = append(domains, domain)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active SSL domains: %w", err)
	}
	if includeDomain != "" {
		if _, exists := seen[includeDomain]; !exists {
			domains = append(domains, includeDomain)
		}
	}
	return domains, nil
}

func (s *Service) activeCertificateOwner(ctx context.Context, websiteID, domain string) (string, int, error) {
	var ownerID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM ssl_certificates
		 WHERE website_id = ? AND domain = ? AND status = 'active'
		 ORDER BY created_at DESC, id DESC LIMIT 1`,
		websiteID, domain,
	).Scan(&ownerID)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("find active certificate owner: %w", err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ssl_certificates
		 WHERE website_id = ? AND domain = ? AND status = 'active'`,
		websiteID, domain,
	).Scan(&count); err != nil {
		return "", 0, fmt.Errorf("count active certificate duplicates: %w", err)
	}
	return ownerID, count, nil
}

func (s *Service) setWebsiteSSLState(ctx context.Context, websiteID string) error {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ssl_certificates WHERE website_id = ? AND status = 'active'`,
		websiteID,
	).Scan(&count); err != nil {
		return fmt.Errorf("count active SSL certificates: %w", err)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE websites SET ssl_enabled = ?, updated_at = ? WHERE id = ?`,
		boolToInt(count > 0), time.Now().UTC().Format(time.RFC3339), websiteID,
	)
	if err != nil {
		return fmt.Errorf("update website SSL state: %w", err)
	}
	return nil
}

func (s *Service) commitActiveCertificate(ctx context.Context, certID, websiteID, issuer string, expiresAt time.Time, autoRenew bool, updatedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin certificate activation: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx,
		`UPDATE ssl_certificates
		 SET issuer = ?, status = 'active', expires_at = ?, auto_renew = ?, error_message = NULL, updated_at = ?
		 WHERE id = ?`,
		issuer, expiresAt.UTC().Format(time.RFC3339), boolToInt(autoRenew), updatedAt.UTC().Format(time.RFC3339), certID,
	)
	if err != nil {
		return fmt.Errorf("activate certificate record: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return model.ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE websites SET ssl_enabled = 1, updated_at = ? WHERE id = ?`,
		updatedAt.UTC().Format(time.RFC3339), websiteID,
	); err != nil {
		return fmt.Errorf("update website SSL state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit certificate activation: %w", err)
	}
	return nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}
