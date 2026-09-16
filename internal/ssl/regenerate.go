package ssl

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	websiteconfig "github.com/mohammadirham37/jenderal_panel/internal/website"
)

// RegenerateTLSVhost re-renders the HTTPS vhost of every actively certified
// domain of the website with the website's current document root. The TLS
// vhost embeds the root, so a document-root change without this regeneration
// leaves HTTPS pointing at the old directory. Websites without active
// certificates are a no-op.
func (s *Service) RegenerateTLSVhost(ctx context.Context, websiteID string) error {
	domains, err := s.activeDomains(ctx, websiteID, "")
	if err != nil {
		return err
	}
	if len(domains) == 0 {
		return nil
	}

	written := false
	for _, domain := range domains {
		site, err := s.loadSiteForDomain(ctx, websiteID, domain)
		if err != nil {
			return err
		}
		if err := s.rewriteTLSVhost(ctx, site, domain, &written); err != nil {
			return err
		}
	}

	if !written {
		return nil
	}
	result, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return fmt.Errorf("nginx config test error: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("nginx config test failed: %s", strings.TrimSpace(result.Stderr))
	}
	return s.runSudoOK(ctx, "systemctl", "reload", "nginx")
}

func (s *Service) rewriteTLSVhost(ctx context.Context, site siteRecord, domain string, written *bool) error {
	domainDir := filepath.Join(s.certDir, domain)
	certPath := filepath.Join(domainDir, "cert.pem")
	keyPath := filepath.Join(domainDir, "key.pem")
	tlsConfPath := "/etc/nginx/sites-available/" + site.Domain + ".ssl"
	tlsEnabledPath := "/etc/nginx/sites-enabled/" + site.Domain + ".ssl"

	// A stale certificate record without material on disk is skipped.
	if probe, err := s.exec.RunSudo(ctx, "test", "-f", certPath); err != nil || probe == nil || probe.ExitCode != 0 {
		return nil
	}

	aliases := strings.Join(site.Aliases, " ")
	base := websiteconfig.VhostData{
		Domain:            site.PrimaryDomain,
		Aliases:           aliases,
		DocumentRoot:      site.DocumentRoot,
		ACMEChallengeRoot: websiteconfig.DefaultACMEChallengeRoot,
		LogDir:            site.LogDir,
		PHPVersion:        site.PHPVersion,
		AppType:           site.AppType,
		Profile:           websiteconfig.NginxProfileFor(site.Framework, site.FrameworkVersion, site.AppType),
		AppPort:           site.AppPort,
		IPv6:              s.ipv6Available(),
		ForceHTTPS:        site.ForceHTTPS,
	}
	tlsConfig, err := websiteconfig.RenderTLSVhost(websiteconfig.TLSVhostData{
		VhostData:       base,
		TLSDomain:       domain,
		CertificatePath: certPath,
		PrivateKeyPath:  keyPath,
	})
	if err != nil {
		return fmt.Errorf("render HTTPS configuration for %s: %w", domain, err)
	}

	// Keep a rollback copy: a failed nginx -t must not leave the site with a
	// broken TLS vhost.
	backupPath := tlsConfPath + ".jenderal-previous"
	backupTaken := false
	if probe, err := s.exec.RunSudo(ctx, "test", "-e", tlsConfPath); err == nil && probe != nil && probe.ExitCode == 0 {
		if _, err := s.exec.RunSudo(ctx, "cp", "-a", tlsConfPath, backupPath); err == nil {
			backupTaken = true
		}
	}
	if err := s.installSystemFile(ctx, []byte(tlsConfig), "0644", tlsConfPath); err != nil {
		return fmt.Errorf("install HTTPS configuration for %s: %w", domain, err)
	}
	if err := s.runSudoOK(ctx, "ln", "-sfn", tlsConfPath, tlsEnabledPath); err != nil {
		return fmt.Errorf("enable HTTPS configuration for %s: %w", domain, err)
	}

	result, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil || result == nil || result.ExitCode != 0 {
		detail := "nginx config test failed"
		if result != nil && strings.TrimSpace(result.Stderr) != "" {
			detail = strings.TrimSpace(result.Stderr)
		} else if err != nil {
			detail = err.Error()
		}
		if backupTaken {
			_, _ = s.exec.RunSudo(ctx, "cp", "-a", backupPath, tlsConfPath)
		} else {
			_, _ = s.exec.RunSudo(ctx, "rm", "-f", tlsConfPath)
		}
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", backupPath)
		return model.NewDomainError("TLS_VHOST_INVALID",
			"regenerated HTTPS configuration for "+domain+" is invalid: "+detail, nil)
	}
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", backupPath)
	*written = true
	return nil
}
