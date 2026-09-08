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
	site, err := s.loadSiteForDomain(ctx, websiteID, domain)
	if err != nil {
		return model.SSLCertificate{}, err
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

	redirectDomains, err := s.activeDomains(ctx, websiteID, domain)
	if err != nil {
		return model.SSLCertificate{}, err
	}
	if err := s.activateCertificate(ctx, activationRequest{Site: site, CertificatePEM: certPEM, PrivateKeyPEM: keyPEM, RedirectDomains: redirectDomains}); err != nil {
		if found {
			if restoreErr := s.restoreCertificateRecord(ctx, existing); restoreErr != nil {
				return model.SSLCertificate{}, fmt.Errorf("install custom certificate: %v; restore record: %w", err, restoreErr)
			}
			return model.SSLCertificate{}, fmt.Errorf("install custom certificate: %w", err)
		}
		message := fmt.Sprintf("install custom certificate: %v", err)
		s.markFailed(ctx, cert.ID, message)
		cert.Status = "failed"
		cert.ErrorMessage = message
		return cert, nil
	}

	updated := time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`UPDATE ssl_certificates
		 SET issuer = 'custom', status = 'active', expires_at = ?, auto_renew = 0,
		     error_message = NULL, updated_at = ? WHERE id = ?`,
		metadata.NotAfter.Format(time.RFC3339), updated.Format(time.RFC3339), cert.ID,
	)
	if err != nil {
		return model.SSLCertificate{}, fmt.Errorf("activate custom certificate record: %w", err)
	}
	if err := s.setWebsiteSSLState(ctx, websiteID); err != nil {
		return model.SSLCertificate{}, err
	}
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
		 FROM ssl_certificates WHERE website_id = ? AND domain = ? ORDER BY created_at DESC LIMIT 1`,
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
	_, err := s.db.ExecContext(ctx,
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

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}
