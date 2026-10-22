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

	"github.com/mohammadirham37/jenderal_panel/internal/model"
	websiteconfig "github.com/mohammadirham37/jenderal_panel/internal/website"
)

type siteRecord struct {
	WebsiteID        string
	PrimaryDomain    string
	Domain           string
	DocumentRoot     string
	PHPVersion       string
	AppType          string
	Framework        string
	FrameworkVersion string
	Status           string
	LogDir           string
	Aliases          []string
	AppPort          int
	ForceHTTPS       bool
	// Fields the website service also renders into the vhost; without them
	// HTTPS (and the HTTP vhost the ssl service rewrites) lost the chosen
	// nginx template, the Octane upstream, and the reverse-proxy target.
	NginxProfile string
	OctanePort   int
	ProxyScheme  string
	ProxyHost    string
	ProxyPort    int
}

// vhostBase builds the vhost data for a site exactly as the website service
// does (internal/website regenerateConfig), so HTTP and HTTPS stay in sync.
// The per-site security snippet is only included when it exists: nginx
// refuses to load a vhost that includes a missing file.
func (s *Service) vhostBase(ctx context.Context, site siteRecord) websiteconfig.VhostData {
	securityInclude := "/etc/nginx/jenderal/security/sites/" + site.WebsiteID + ".conf"
	if probe, err := s.exec.RunSudo(ctx, "test", "-f", securityInclude); err != nil || probe == nil || probe.ExitCode != 0 {
		securityInclude = ""
	}
	return websiteconfig.VhostData{
		Domain:            site.PrimaryDomain,
		Aliases:           strings.Join(site.Aliases, " "),
		DocumentRoot:      site.DocumentRoot,
		ACMEChallengeRoot: websiteconfig.DefaultACMEChallengeRoot,
		LogDir:            site.LogDir,
		PHPVersion:        site.PHPVersion,
		AppType:           site.AppType,
		Profile: websiteconfig.NginxProfileForWebsite(model.Website{
			NginxProfile: site.NginxProfile, Framework: site.Framework,
			FrameworkVersion: site.FrameworkVersion, AppType: site.AppType,
		}),
		IPv6:            s.ipv6Available(),
		ForceHTTPS:      site.ForceHTTPS,
		SecurityInclude: securityInclude,
		OctanePort:      site.OctanePort,
		AppPort:         site.AppPort,
		ProxyScheme:     site.ProxyScheme,
		ProxyHost:       site.ProxyHost,
		ProxyPort:       site.ProxyPort,
	}
}

type activationRequest struct {
	Site            siteRecord
	CertificatePEM  []byte
	PrivateKeyPEM   []byte
	RedirectDomains []string
}

type backupEntry struct {
	path       string
	backupPath string
	existed    bool
}

type activationSession struct {
	service   *Service
	backupDir string
	backups   []backupEntry
}

func (s *Service) loadSiteForDomain(ctx context.Context, websiteID, domain string) (siteRecord, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	var site siteRecord
	var webUser string
	var phpVersion, framework, frameworkVersion sql.NullString
	var forceHTTPS int
	err := s.db.QueryRowContext(ctx,
		`SELECT w.id, w.domain, d.name, w.document_root, w.php_version, w.app_type, w.status, w.web_user,
		        w.framework, w.framework_version, w.app_port, w.force_https,
		        COALESCE(w.nginx_profile, ''), COALESCE(w.octane_port, 0),
		        COALESCE(w.proxy_scheme, ''), COALESCE(w.proxy_host, ''), COALESCE(w.proxy_port, 0)
		 FROM websites w
		 JOIN domains d ON d.website_id = w.id
		 WHERE w.id = ? AND d.name = ?`,
		websiteID, domain,
	).Scan(
		&site.WebsiteID, &site.PrimaryDomain, &site.Domain, &site.DocumentRoot,
		&phpVersion, &site.AppType, &site.Status, &webUser, &framework, &frameworkVersion,
		&site.AppPort, &forceHTTPS,
		&site.NginxProfile, &site.OctanePort, &site.ProxyScheme, &site.ProxyHost, &site.ProxyPort,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return siteRecord{}, model.NewValidationError("domain is not registered to the selected website")
		}
		return siteRecord{}, fmt.Errorf("load website domain: %w", err)
	}
	site.PHPVersion = phpVersion.String
	site.Framework = framework.String
	site.FrameworkVersion = frameworkVersion.String
	site.LogDir = "/home/" + webUser + "/logs"
	site.ForceHTTPS = forceHTTPS == 1

	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM domains WHERE website_id = ? AND type != 'primary' ORDER BY created_at ASC`,
		websiteID,
	)
	if err != nil {
		return siteRecord{}, fmt.Errorf("load website aliases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return siteRecord{}, fmt.Errorf("scan website alias: %w", err)
		}
		site.Aliases = append(site.Aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return siteRecord{}, fmt.Errorf("load website aliases: %w", err)
	}

	return site, nil
}

func (s *Service) activateCertificate(ctx context.Context, req activationRequest) error {
	session, err := s.beginCertificateActivation(ctx, req)
	if err != nil {
		return err
	}
	session.commit()
	return nil
}

func (s *Service) beginCertificateActivation(ctx context.Context, req activationRequest) (*activationSession, error) {
	domainDir := filepath.Join(s.certDir, req.Site.Domain)
	certPath := filepath.Join(domainDir, "cert.pem")
	keyPath := filepath.Join(domainDir, "key.pem")
	httpConfPath := "/etc/nginx/sites-available/" + req.Site.PrimaryDomain
	httpEnabledPath := "/etc/nginx/sites-enabled/" + req.Site.PrimaryDomain
	tlsConfPath := "/etc/nginx/sites-available/" + req.Site.Domain + ".ssl"
	tlsEnabledPath := "/etc/nginx/sites-enabled/" + req.Site.Domain + ".ssl"

	backupDir := "/tmp/jenderal_ssl_backup_" + ulid.Make().String()
	if err := s.runSudoOK(ctx, "mkdir", "-m", "0700", backupDir); err != nil {
		return nil, fmt.Errorf("create SSL backup: %w", err)
	}

	paths := []string{domainDir, httpConfPath, httpEnabledPath, tlsConfPath, tlsEnabledPath}
	backups, err := s.snapshotPaths(ctx, backupDir, paths)
	if err != nil {
		_, _ = s.exec.RunSudo(context.Background(), "rm", "-rf", backupDir)
		return nil, err
	}
	session := &activationSession{service: s, backupDir: backupDir, backups: backups}

	fail := func(cause error) (*activationSession, error) {
		if restoreErr := session.rollback(); restoreErr != nil {
			return nil, fmt.Errorf("%v; rollback failed: %w", cause, restoreErr)
		}
		return nil, cause
	}

	if err := s.runSudoOK(ctx, "mkdir", "-p", domainDir); err != nil {
		return fail(fmt.Errorf("create certificate directory: %w", err))
	}
	if err := s.installSystemFile(ctx, req.CertificatePEM, "0644", certPath); err != nil {
		return fail(fmt.Errorf("install certificate: %w", err))
	}
	if err := s.installSystemFile(ctx, req.PrivateKeyPEM, "0600", keyPath); err != nil {
		return fail(fmt.Errorf("install private key: %w", err))
	}

	base := s.vhostBase(ctx, req.Site)
	base.RedirectDomains = req.RedirectDomains
	httpConfig, err := websiteconfig.RenderVhost(base)
	if err != nil {
		return fail(fmt.Errorf("render HTTP configuration: %w", err))
	}
	tlsConfig, err := websiteconfig.RenderTLSVhost(websiteconfig.TLSVhostData{
		VhostData:       base,
		TLSDomain:       req.Site.Domain,
		CertificatePath: certPath,
		PrivateKeyPath:  keyPath,
	})
	if err != nil {
		return fail(fmt.Errorf("render HTTPS configuration: %w", err))
	}
	if err := s.installSystemFile(ctx, []byte(httpConfig), "0644", httpConfPath); err != nil {
		return fail(fmt.Errorf("install HTTP configuration: %w", err))
	}
	if err := s.installSystemFile(ctx, []byte(tlsConfig), "0644", tlsConfPath); err != nil {
		return fail(fmt.Errorf("install HTTPS configuration: %w", err))
	}
	if err := s.runSudoOK(ctx, "ln", "-sfn", httpConfPath, httpEnabledPath); err != nil {
		return fail(fmt.Errorf("enable HTTP configuration: %w", err))
	}
	if err := s.runSudoOK(ctx, "ln", "-sfn", tlsConfPath, tlsEnabledPath); err != nil {
		return fail(fmt.Errorf("enable HTTPS configuration: %w", err))
	}

	result, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return fail(fmt.Errorf("nginx config test error: %w", err))
	}
	if result.ExitCode != 0 {
		return fail(fmt.Errorf("nginx config test failed: %s", strings.TrimSpace(result.Stderr)))
	}
	result, err = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")
	if err != nil {
		return fail(fmt.Errorf("reload nginx: %w", err))
	}
	if result.ExitCode != 0 {
		return fail(fmt.Errorf("reload nginx: %s", strings.TrimSpace(result.Stderr)))
	}

	return session, nil
}

func (a *activationSession) commit() {
	_, _ = a.service.exec.RunSudo(context.Background(), "rm", "-rf", a.backupDir)
}

func (a *activationSession) rollback() error {
	cleanupCtx, cancel := cleanupContext()
	defer cancel()
	if err := a.service.restorePaths(cleanupCtx, a.backups); err != nil {
		return fmt.Errorf("%w (backup retained at %s)", err, a.backupDir)
	}
	_, _ = a.service.exec.RunSudo(context.Background(), "rm", "-rf", a.backupDir)
	return nil
}

func (s *Service) syncHTTPConfig(ctx context.Context, site siteRecord, redirectDomains []string) error {
	data := s.vhostBase(ctx, site)
	data.RedirectDomains = redirectDomains
	config, err := websiteconfig.RenderVhost(data)
	if err != nil {
		return fmt.Errorf("render HTTP configuration: %w", err)
	}
	confPath := "/etc/nginx/sites-available/" + site.PrimaryDomain
	if err := s.installSystemFile(ctx, []byte(config), "0644", confPath); err != nil {
		return fmt.Errorf("install HTTP configuration: %w", err)
	}
	enabledPath := "/etc/nginx/sites-enabled/" + site.PrimaryDomain
	if site.Status == "suspended" {
		enabledPath += ".suspended"
	}
	if err := s.runSudoOK(ctx, "ln", "-sfn", confPath, enabledPath); err != nil {
		return fmt.Errorf("enable HTTP configuration: %w", err)
	}
	return nil
}

func (s *Service) removeCertificateConfig(ctx context.Context, site siteRecord, redirectDomains []string) error {
	paths := []string{
		filepath.Join(s.certDir, site.Domain),
		"/etc/nginx/sites-available/" + site.PrimaryDomain,
		"/etc/nginx/sites-enabled/" + site.PrimaryDomain,
		"/etc/nginx/sites-enabled/" + site.PrimaryDomain + ".suspended",
		"/etc/nginx/sites-available/" + site.Domain + ".ssl",
		"/etc/nginx/sites-enabled/" + site.Domain + ".ssl",
		"/etc/nginx/sites-enabled/" + site.Domain + ".ssl.suspended",
	}
	backupDir := "/tmp/jenderal_ssl_backup_" + ulid.Make().String()
	if err := s.runSudoOK(ctx, "mkdir", "-m", "0700", backupDir); err != nil {
		return fmt.Errorf("create SSL backup: %w", err)
	}

	backups, err := s.snapshotPaths(ctx, backupDir, paths)
	if err != nil {
		_, _ = s.exec.RunSudo(context.Background(), "rm", "-rf", backupDir)
		return err
	}
	rollback := func(cause error) error {
		cleanupCtx, cancel := cleanupContext()
		defer cancel()
		if restoreErr := s.restorePaths(cleanupCtx, backups); restoreErr != nil {
			return fmt.Errorf("%v; rollback failed: %w (backup retained at %s)", cause, restoreErr, backupDir)
		}
		_, _ = s.exec.RunSudo(context.Background(), "rm", "-rf", backupDir)
		return cause
	}

	for _, path := range []string{paths[0], paths[4], paths[5], paths[6]} {
		if err := s.runSudoOK(ctx, "rm", "-rf", path); err != nil {
			return rollback(fmt.Errorf("remove %s: %w", path, err))
		}
	}
	if err := s.syncHTTPConfig(ctx, site, redirectDomains); err != nil {
		return rollback(err)
	}
	result, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return rollback(fmt.Errorf("test nginx configuration: %w", err))
	}
	if result.ExitCode != 0 {
		return rollback(fmt.Errorf("nginx config test failed: %s", strings.TrimSpace(result.Stderr)))
	}
	if err := s.runSudoOK(ctx, "systemctl", "reload", "nginx"); err != nil {
		return rollback(fmt.Errorf("reload nginx: %w", err))
	}
	_, _ = s.exec.RunSudo(context.Background(), "rm", "-rf", backupDir)
	return nil
}

func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

func (s *Service) snapshotPaths(ctx context.Context, backupDir string, paths []string) ([]backupEntry, error) {
	entries := make([]backupEntry, 0, len(paths))
	for i, path := range paths {
		entry := backupEntry{path: path, backupPath: filepath.Join(backupDir, fmt.Sprintf("%d", i))}
		result, err := s.exec.RunSudo(ctx, "test", "-e", path)
		if err != nil {
			return nil, fmt.Errorf("check existing path %s: %w", path, err)
		}
		if result.ExitCode == 0 {
			entry.existed = true
			if err := s.runSudoOK(ctx, "cp", "-a", path, entry.backupPath); err != nil {
				return nil, fmt.Errorf("back up %s: %w", path, err)
			}
		} else if result.ExitCode != 1 {
			return nil, fmt.Errorf("check existing path %s: exit status %d", path, result.ExitCode)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *Service) restorePaths(ctx context.Context, entries []backupEntry) error {
	for _, entry := range entries {
		if err := s.runSudoOK(ctx, "rm", "-rf", entry.path); err != nil {
			return err
		}
		if entry.existed {
			if err := s.runSudoOK(ctx, "cp", "-a", entry.backupPath, entry.path); err != nil {
				return err
			}
		}
	}

	result, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return fmt.Errorf("validate restored nginx configuration: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("restored nginx configuration is invalid: %s", strings.TrimSpace(result.Stderr))
	}
	if err := s.runSudoOK(ctx, "systemctl", "reload", "nginx"); err != nil {
		return fmt.Errorf("reload restored nginx configuration: %w", err)
	}
	return nil
}

func (s *Service) installSystemFile(ctx context.Context, content []byte, mode, target string) error {
	tmpPath, err := writeTempMaterial("jenderal_ssl_*.tmp", content)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath)
	return s.runSudoOK(ctx, "install", "-m", mode, tmpPath, target)
}

func writeTempMaterial(pattern string, data []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Chmod(0600); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func (s *Service) runSudoOK(ctx context.Context, name string, args ...string) error {
	result, err := s.exec.RunSudo(ctx, name, args...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = fmt.Sprintf("exit status %d", result.ExitCode)
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}
