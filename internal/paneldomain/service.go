// Package paneldomain serves the panel itself over HTTPS on a chosen domain:
// it writes an nginx vhost that reverse-proxies to the local panel port,
// obtains a Let's Encrypt certificate for the domain, rewrites the vhost to
// HTTPS (keeping websocket and ACME support), renews the certificate before
// expiry, and can remove everything again.
package paneldomain

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/ssl"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

const (
	// Upstream is the local panel address the vhost proxies to. The panel
	// listens on plain HTTP here by default.
	Upstream = "127.0.0.1:8443"

	// certDir holds the panel domain certificate and key.
	certDir  = "/var/lib/jenderal/panel-domain"
	certFile = certDir + "/panel.crt"
	keyFile  = certDir + "/panel.key"

	acmeWebroot = "/var/lib/jenderal/acme-challenges"

	settingDomain  = "panel_domain"
	settingEmail   = "panel_domain_email"
	settingEnabled = "panel_domain_enabled"

	renewBefore = 30 * 24 * time.Hour
	vhostSuffix = ".conf"
)

var domainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// Status is the API view of the panel domain configuration.
type Status struct {
	Domain       string `json:"domain"`
	Email        string `json:"email"`
	Enabled      bool   `json:"enabled"`
	VhostPresent bool   `json:"vhost_present"`
	CertExpiry   string `json:"cert_expiry,omitempty"`
}

// Service manages the panel domain vhost, certificate, and renewal.
type Service struct {
	db    *sql.DB
	exec  executor.CommandExecutor
	audit *audit.Service
	tasks *taskrunner.Runner
}

// NewService creates a new paneldomain Service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc}
}

// SetTaskRunner wires the task runner so setup/renew run as visible tasks.
func (s *Service) SetTaskRunner(tr *taskrunner.Runner) { s.tasks = tr }

func vhostAvailablePath(domain string) string {
	return "/etc/nginx/sites-available/jenderal-panel-" + domain + vhostSuffix
}

func vhostEnabledPath(domain string) string {
	return "/etc/nginx/sites-enabled/jenderal-panel-" + domain + vhostSuffix
}

func (s *Service) loadConfig(ctx context.Context) (domain, email string, enabled bool, err error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value FROM settings WHERE key IN (?, ?, ?)`,
		settingDomain, settingEmail, settingEnabled)
	if err != nil {
		return "", "", false, fmt.Errorf("read panel domain config: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return "", "", false, err
		}
		switch key {
		case settingDomain:
			domain = value
		case settingEmail:
			email = value
		case settingEnabled:
			enabled = value == "1"
		}
	}
	return domain, email, enabled, rows.Err()
}

func (s *Service) saveSettings(ctx context.Context, pairs map[string]string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for key, value := range pairs {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			key, value, now,
		); err != nil {
			return fmt.Errorf("save %s: %w", key, err)
		}
	}
	return nil
}

// Status reports the current panel domain configuration and certificate
// expiry (parsed from the installed certificate when present).
func (s *Service) Status(ctx context.Context) (Status, error) {
	domain, email, enabled, err := s.loadConfig(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{Domain: domain, Email: email, Enabled: enabled}
	if domain != "" {
		if _, err := os.Stat(vhostAvailablePath(domain)); err == nil {
			st.VhostPresent = true
		}
	}
	if data, err := os.ReadFile(certFile); err == nil {
		if block, _ := pem.Decode(data); block != nil {
			if leaf, err := x509.ParseCertificate(block.Bytes); err == nil {
				st.CertExpiry = leaf.NotAfter.UTC().Format(time.RFC3339)
			}
		}
	}
	return st, nil
}

// ─── Vhost rendering ──────────────────────────────────────────────

var wsVarSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func wsMapVar(domain string, tls bool) string {
	suffix := wsVarSanitizer.ReplaceAllString(domain, "_")
	if tls {
		return suffix + "_wss"
	}
	return suffix + "_ws"
}

const proxyLocation = `    location / {
        proxy_pass http://` + Upstream + `;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }`

func acmeLocation(webroot string) string {
	return `    location ^~ /.well-known/acme-challenge/ {
        root ` + webroot + `;
        try_files $uri =404;
    }`
}

// renderHTTPVhost renders the port-80 vhost: ACME challenges plus a plain
// proxy so the domain works even before the certificate exists.
func renderHTTPVhost(domain, webroot string) string {
	mapVar := "$jenderal_ws_" + wsVarSanitizer.ReplaceAllString(domain, "_")
	return `# Managed by Jenderal Panel (panel domain)
map $http_upgrade ` + mapVar + ` {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    server_name ` + domain + `;

` + acmeLocation(webroot) + `

    location / {
        proxy_pass http://` + Upstream + `;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection ` + mapVar + `;
        proxy_read_timeout 300s;
    }
}
`
}

// renderTLSVhost renders the HTTPS vhost with websocket-aware proxying.
func renderTLSVhost(domain, certPath, keyPath, webroot string) string {
	mapVar := "$jenderal_ws_" + wsVarSanitizer.ReplaceAllString(domain, "_") + "_wss"
	return `# Managed by Jenderal Panel (panel domain)
map $http_upgrade ` + mapVar + ` {
    default upgrade;
    ''      close;
}

server {
    listen 443 ssl;
    server_name ` + domain + `;

    ssl_certificate ` + certPath + `;
    ssl_certificate_key ` + keyPath + `;
    ssl_protocols TLSv1.2 TLSv1.3;

` + acmeLocation(webroot) + `

    location / {
        proxy_pass http://` + Upstream + `;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection ` + mapVar + `;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
`
}

// ─── Nginx helpers ────────────────────────────────────────────────

// writeVhostTested writes content to the vhost path, runs nginx -t, and
// restores the previous file when the test fails.
func (s *Service) writeVhostTested(ctx context.Context, path, content string) error {
	previous := ""
	if result, err := s.exec.RunSudo(ctx, "cat", path); err == nil && result.ExitCode == 0 {
		previous = result.Stdout
	}
	if _, err := s.exec.RunSudoWithInput(ctx, content, "tee", path); err != nil {
		return fmt.Errorf("write vhost: %w", err)
	}
	if err := s.testAndRestoreNginx(ctx, path, previous); err != nil {
		return err
	}
	return nil
}

func (s *Service) removeVhostTested(ctx context.Context, path string) error {
	previous := ""
	if result, err := s.exec.RunSudo(ctx, "cat", path); err == nil && result.ExitCode == 0 {
		previous = result.Stdout
	}
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", path)
	if err := s.testAndRestoreNginx(ctx, path, previous); err != nil {
		return err
	}
	return nil
}

// testAndRestoreNginx validates the nginx configuration, restoring fallback
// content (when non-empty) if the test fails.
func (s *Service) testAndRestoreNginx(ctx context.Context, restoredPath, fallbackContent string) error {
	result, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return fmt.Errorf("run nginx -t: %w", err)
	}
	if result.ExitCode != 0 {
		if fallbackContent != "" {
			_, _ = s.exec.RunSudoWithInput(ctx, fallbackContent, "tee", restoredPath)
		} else {
			_, _ = s.exec.RunSudo(ctx, "rm", "-f", restoredPath)
		}
		_, _ = s.exec.RunSudo(ctx, "nginx", "-t")
		return fmt.Errorf("nginx -t failed: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (s *Service) reloadNginx(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")
	if err != nil {
		return fmt.Errorf("reload nginx: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("reload nginx failed: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (s *Service) enableVhost(ctx context.Context, domain string) error {
	if _, err := s.exec.RunSudo(ctx, "ln", "-sf",
		vhostAvailablePath(domain), vhostEnabledPath(domain)); err != nil {
		return fmt.Errorf("enable vhost: %w", err)
	}
	return nil
}

// ─── Setup / Disable / Renew ──────────────────────────────────────

var panelDomainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// Setup provisions the panel domain end to end as a background task: HTTP
// vhost, certificate, HTTPS vhost. The domain/email are persisted up front
// so status reflects the intent even before the task finishes.
func (s *Service) Setup(ctx context.Context, domain, email string) (string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	email = strings.TrimSpace(email)
	if !panelDomainRegex.MatchString(domain) {
		return "", model.NewValidationError("invalid panel domain: " + domain)
	}
	if email == "" {
		return "", model.NewValidationError("email is required for the certificate")
	}
	if err := s.saveSettings(ctx, map[string]string{
		settingDomain:  domain,
		settingEmail:   email,
		settingEnabled: "1",
	}); err != nil {
		return "", err
	}

	if s.tasks == nil {
		return "", model.NewDomainError("TASK_RUNNER_UNAVAILABLE", "task runner is not available", nil)
	}
	return s.tasks.RunFuncWithOptions(
		taskrunner.Options{Name: "Set up panel domain " + domain, Module: "settings", Timeout: 30 * time.Minute},
		func(taskCtx context.Context, write func(string)) error {
			return s.setup(taskCtx, domain, email, write)
		}), nil
}

func (s *Service) setup(ctx context.Context, domain, email string, write func(string)) error {
	webroot := acmeWebroot
	vhostPath := vhostAvailablePath(domain)

	// 1. HTTP vhost (ACME + proxy), tested and reloaded.
	write("Writing HTTP vhost…")
	if err := s.writeVhostTested(ctx, vhostPath, renderHTTPVhost(domain, webroot)); err != nil {
		return err
	}
	if err := s.enableVhost(ctx, domain); err != nil {
		return err
	}
	if err := s.reloadNginx(ctx); err != nil {
		return err
	}

	// 2. Certificate via HTTP-01 webroot through the shared ACME account.
	write("Requesting Let's Encrypt certificate (make sure the DNS A record points here)…")
	if _, err := os.Stat(webroot); err != nil {
		if _, err := s.exec.RunSudo(ctx, "mkdir", "-p", webroot); err != nil {
			return fmt.Errorf("create acme webroot: %w", err)
		}
	}
	lego := ssl.NewLegoClient(email, "/var/lib/jenderal/acme")
	certPEM, keyPEM, err := lego.ObtainCertificate(domain, webroot)
	if err != nil {
		return fmt.Errorf("obtain certificate: %w", err)
	}

	if _, err := s.exec.RunSudoWithInput(ctx, string(certPEM), "tee", certFile); err != nil {
		return fmt.Errorf("write certificate: %w", err)
	}
	if _, err := s.exec.RunSudoWithInput(ctx, string(keyPEM), "tee", keyFile); err != nil {
		return fmt.Errorf("write certificate key: %w", err)
	}
	_, _ = s.exec.RunSudo(ctx, "chmod", "0600", keyFile)

	// 3. HTTPS vhost.
	write("Writing HTTPS vhost…")
	if err := s.writeVhostTested(ctx, vhostPath, renderTLSVhost(domain, certFile, keyFile, webroot)); err != nil {
		return err
	}
	if err := s.reloadNginx(ctx); err != nil {
		return err
	}

	write("Panel domain ready: https://" + domain)
	return nil
}

// Disable removes the panel domain vhost and marks the feature off. The
// certificate files are kept on disk (harmless, useful for re-setup).
func (s *Service) Disable(ctx context.Context, domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return model.NewValidationError("panel domain is not configured")
	}
	if err := s.removeVhostTested(ctx, vhostEnabledPath(domain)); err != nil {
		return err
	}
	if err := s.removeVhostTested(ctx, vhostAvailablePath(domain)); err != nil {
		return err
	}
	if err := s.reloadNginx(ctx); err != nil {
		return err
	}
	if err := s.saveSettings(ctx, map[string]string{settingEnabled: "0"}); err != nil {
		return err
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "panel_domain_disable", Module: "settings", Target: domain, Detail: "removed panel domain vhost"})
	return nil
}

// Renew re-obtains the certificate for the configured panel domain and
// reloads nginx. No vhost changes are needed.
func (s *Service) Renew(ctx context.Context) (string, error) {
	domain, email, enabled, err := s.loadConfig(ctx)
	if err != nil {
		return "", err
	}
	if !enabled || domain == "" {
		return "", model.NewValidationError("panel domain is not enabled")
	}
	if s.tasks == nil {
		return "", model.NewDomainError("TASK_RUNNER_UNAVAILABLE", "task runner is not available", nil)
	}
	return s.tasks.RunFuncWithOptions(
		taskrunner.Options{Name: "Renew panel domain certificate", Module: "settings", Timeout: 30 * time.Minute},
		func(taskCtx context.Context, write func(string)) error {
			webroot := acmeWebroot
			lego := ssl.NewLegoClient(email, "/var/lib/jenderal/acme")
			write("Requesting renewed certificate…")
			certPEM, keyPEM, err := lego.ObtainCertificate(domain, webroot)
			if err != nil {
				return fmt.Errorf("obtain certificate: %w", err)
			}
			if _, err := s.exec.RunSudoWithInput(taskCtx, string(certPEM), "tee", certFile); err != nil {
				return fmt.Errorf("write certificate: %w", err)
			}
			if _, err := s.exec.RunSudoWithInput(taskCtx, string(keyPEM), "tee", keyFile); err != nil {
				return fmt.Errorf("write certificate key: %w", err)
			}
			if err := s.reloadNginx(taskCtx); err != nil {
				return err
			}
			write("Certificate renewed.")
			return nil
		}), nil
}

// Start launches the daily renewal sweep: when the installed certificate
// expires within 30 days, it is renewed automatically.
func (s *Service) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.renewIfDue(context.Background())
			}
		}
	}()
}

func (s *Service) renewIfDue(ctx context.Context) {
	domain, _, enabled, err := s.loadConfig(ctx)
	if err != nil || !enabled || domain == "" {
		return
	}
	data, err := os.ReadFile(certFile)
	if err != nil {
		return
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return
	}
	if time.Until(leaf.NotAfter) > renewBefore {
		return
	}
	if _, err := s.Renew(ctx); err != nil {
		log.Printf("panel domain renewal: %v", err)
	}
}
