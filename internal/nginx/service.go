package nginx

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/landing"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Service manages Nginx installation, configuration, and site management.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new Nginx management service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// Status returns the current status of the Nginx service.
func (s *Service) Status(ctx context.Context) (*model.NginxStatus, error) {
	status := &model.NginxStatus{}

	// Check if nginx is installed and get version (version goes to stderr).
	vResult, err := s.exec.Run(ctx, "nginx", "-v")
	if err != nil {
		return nil, fmt.Errorf("check nginx version: %w", err)
	}
	if vResult.ExitCode != 0 {
		// nginx not installed
		return status, nil
	}
	status.Installed = true
	status.Version = parseVersion(vResult.Stderr)

	// Check config validity.
	tResult, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return nil, fmt.Errorf("test nginx config: %w", err)
	}
	status.ConfigOK = tResult.ExitCode == 0

	// Get service status via systemctl show.
	sResult, err := s.exec.RunSudo(ctx, "systemctl", "show",
		"--property=ActiveState,SubState,MainPID,UnitFileState", "nginx")
	if err != nil {
		return nil, fmt.Errorf("systemctl show nginx: %w", err)
	}
	if sResult.ExitCode == 0 {
		props := parseProps(sResult.Stdout)
		status.Running = props["SubState"] == "running"
		status.Enabled = props["UnitFileState"] == "enabled"
		if pidStr, ok := props["MainPID"]; ok {
			if pid, e := strconv.Atoi(pidStr); e == nil {
				status.PID = pid
			}
		}
	}

	return status, nil
}

// Install installs Nginx using apt-get.
func (s *Service) Install(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "apt-get", "install", "-y", "nginx")
	if err != nil {
		return fmt.Errorf("install nginx: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("install nginx: %s", strings.TrimSpace(result.Stderr))
	}
	result, err = s.exec.RunSudoWithInput(ctx, landing.CSS(), "tee", "--", "/var/www/html/jenderal-landing.css")
	if err != nil {
		return fmt.Errorf("install nginx landing stylesheet: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("install nginx landing stylesheet: %s", strings.TrimSpace(result.Stderr))
	}
	result, err = s.exec.RunSudoWithInput(ctx, landing.NginxWelcome(), "tee", "--", "/var/www/html/index.nginx-debian.html")
	if err != nil {
		return fmt.Errorf("install nginx welcome page: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("install nginx welcome page: %s", strings.TrimSpace(result.Stderr))
	}
	result, err = s.exec.RunSudo(ctx, "chmod", "0644", "/var/www/html/index.nginx-debian.html", "/var/www/html/jenderal-landing.css")
	if err != nil {
		return fmt.Errorf("set nginx welcome page permissions: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("set nginx welcome page permissions: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Start starts the Nginx service.
func (s *Service) Start(ctx context.Context) error {
	return s.systemctlAction(ctx, "start")
}

// Stop stops the Nginx service.
func (s *Service) Stop(ctx context.Context) error {
	return s.systemctlAction(ctx, "stop")
}

// Restart restarts the Nginx service.
func (s *Service) Restart(ctx context.Context) error {
	return s.systemctlAction(ctx, "restart")
}

// Reload reloads the Nginx service configuration.
func (s *Service) Reload(ctx context.Context) error {
	return s.systemctlAction(ctx, "reload")
}

// systemctlAction runs a systemctl command for the nginx service.
func (s *Service) systemctlAction(ctx context.Context, action string) error {
	result, err := s.exec.RunSudo(ctx, "systemctl", action, "nginx")
	if err != nil {
		return fmt.Errorf("%s nginx: %w", action, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("%s nginx: %s", action, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// TestConfig tests the Nginx configuration. Returns whether the config is
// valid, the full output from nginx -t, and any execution error.
func (s *Service) TestConfig(ctx context.Context) (bool, string, error) {
	result, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return false, "", fmt.Errorf("test nginx config: %w", err)
	}
	return result.ExitCode == 0, result.Stderr, nil
}

// GetMainConfig returns the contents of /etc/nginx/nginx.conf.
func (s *Service) GetMainConfig(ctx context.Context) (string, error) {
	result, err := s.exec.RunSudo(ctx, "cat", "/etc/nginx/nginx.conf")
	if err != nil {
		return "", fmt.Errorf("read nginx.conf: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read nginx.conf: %s", strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// SaveMainConfig writes content to /etc/nginx/nginx.conf with backup and
// validation. If the new config is invalid, the backup is restored and a
// DomainError with code NGINX_CONFIG_INVALID is returned.
func (s *Service) SaveMainConfig(ctx context.Context, content string) error {
	// Write content to a temporary file.
	tmpPath := "/tmp/jenderal_nginx.tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	// Backup current config.
	result, err := s.exec.RunSudo(ctx, "cp", "/etc/nginx/nginx.conf", "/etc/nginx/nginx.conf.bak")
	if err != nil {
		return fmt.Errorf("backup nginx.conf: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("backup nginx.conf: %s", strings.TrimSpace(result.Stderr))
	}

	// Copy temp file to nginx.conf.
	result, err = s.exec.RunSudo(ctx, "cp", tmpPath, "/etc/nginx/nginx.conf")
	if err != nil {
		return fmt.Errorf("write nginx.conf: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write nginx.conf: %s", strings.TrimSpace(result.Stderr))
	}

	// Validate the new config.
	valid, output, err := s.TestConfig(ctx)
	if err != nil {
		return err
	}
	if !valid {
		// Restore from backup.
		_, _ = s.exec.RunSudo(ctx, "cp", "/etc/nginx/nginx.conf.bak", "/etc/nginx/nginx.conf")
		return model.NewDomainError("NGINX_CONFIG_INVALID",
			"nginx configuration is invalid: "+strings.TrimSpace(output), nil)
	}

	// Config is valid, reload nginx.
	return s.Reload(ctx)
}

// ListSites returns a list of site configurations from sites-available,
// indicating whether each is enabled (has a symlink in sites-enabled).
func (s *Service) ListSites(ctx context.Context) ([]model.SiteConfig, error) {
	// List sites-available.
	availResult, err := s.exec.RunSudo(ctx, "ls", "/etc/nginx/sites-available")
	if err != nil {
		return nil, fmt.Errorf("list sites-available: %w", err)
	}
	if availResult.ExitCode != 0 {
		return nil, fmt.Errorf("list sites-available: %s", strings.TrimSpace(availResult.Stderr))
	}

	// List sites-enabled.
	enabledResult, err := s.exec.RunSudo(ctx, "ls", "/etc/nginx/sites-enabled")
	if err != nil {
		return nil, fmt.Errorf("list sites-enabled: %w", err)
	}

	enabledSet := make(map[string]bool)
	if enabledResult.ExitCode == 0 {
		for _, name := range splitLines(enabledResult.Stdout) {
			enabledSet[name] = true
		}
	}

	var sites []model.SiteConfig
	for _, name := range splitLines(availResult.Stdout) {
		sites = append(sites, model.SiteConfig{
			Name:    name,
			Path:    "/etc/nginx/sites-available/" + name,
			Enabled: enabledSet[name],
		})
	}

	return sites, nil
}

// GetSiteConfig returns the contents of a site configuration file.
func (s *Service) GetSiteConfig(ctx context.Context, name string) (string, error) {
	if err := validateSiteName(name); err != nil {
		return "", err
	}

	result, err := s.exec.RunSudo(ctx, "cat", "/etc/nginx/sites-available/"+name)
	if err != nil {
		return "", fmt.Errorf("read site config %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read site config %s: %s", name, strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// SaveSiteConfig writes content to a site configuration file with backup and
// validation. If the new config is invalid, the backup is restored.
func (s *Service) SaveSiteConfig(ctx context.Context, name, content string) error {
	if err := validateSiteName(name); err != nil {
		return err
	}

	sitePath := "/etc/nginx/sites-available/" + name
	bakPath := sitePath + ".bak"
	tmpPath := "/tmp/jenderal_nginx_site.tmp"

	// Write content to temp file.
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp site config: %w", err)
	}

	// Backup current config (may not exist for new sites, ignore errors).
	_, _ = s.exec.RunSudo(ctx, "cp", sitePath, bakPath)

	// Copy temp file to sites-available.
	result, err := s.exec.RunSudo(ctx, "cp", tmpPath, sitePath)
	if err != nil {
		return fmt.Errorf("write site config %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write site config %s: %s", name, strings.TrimSpace(result.Stderr))
	}

	// Validate config.
	valid, output, err := s.TestConfig(ctx)
	if err != nil {
		return err
	}
	if !valid {
		// Restore from backup.
		_, _ = s.exec.RunSudo(ctx, "cp", bakPath, sitePath)
		return model.NewDomainError("NGINX_CONFIG_INVALID",
			"nginx configuration is invalid: "+strings.TrimSpace(output), nil)
	}

	// Config is valid, reload nginx.
	return s.Reload(ctx)
}

// EnableSite creates a symlink in sites-enabled pointing to sites-available.
func (s *Service) EnableSite(ctx context.Context, name string) error {
	if err := validateSiteName(name); err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "ln", "-sf",
		"/etc/nginx/sites-available/"+name,
		"/etc/nginx/sites-enabled/"+name)
	if err != nil {
		return fmt.Errorf("enable site %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("enable site %s: %s", name, strings.TrimSpace(result.Stderr))
	}

	// Validate and reload.
	valid, output, err := s.TestConfig(ctx)
	if err != nil {
		return err
	}
	if valid {
		return s.Reload(ctx)
	}
	return model.NewDomainError("NGINX_CONFIG_INVALID",
		"nginx configuration is invalid after enabling site: "+strings.TrimSpace(output), nil)
}

// DisableSite removes the symlink from sites-enabled.
func (s *Service) DisableSite(ctx context.Context, name string) error {
	if err := validateSiteName(name); err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "rm", "-f", "/etc/nginx/sites-enabled/"+name)
	if err != nil {
		return fmt.Errorf("disable site %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("disable site %s: %s", name, strings.TrimSpace(result.Stderr))
	}

	// Validate and reload.
	valid, output, err := s.TestConfig(ctx)
	if err != nil {
		return err
	}
	if valid {
		return s.Reload(ctx)
	}
	return model.NewDomainError("NGINX_CONFIG_INVALID",
		"nginx configuration is invalid after disabling site: "+strings.TrimSpace(output), nil)
}

// DeleteSite removes a site from both sites-available and sites-enabled.
func (s *Service) DeleteSite(ctx context.Context, name string) error {
	if err := validateSiteName(name); err != nil {
		return err
	}

	// Remove from sites-enabled first.
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", "/etc/nginx/sites-enabled/"+name)

	// Remove from sites-available.
	result, err := s.exec.RunSudo(ctx, "rm", "-f", "/etc/nginx/sites-available/"+name)
	if err != nil {
		return fmt.Errorf("delete site %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("delete site %s: %s", name, strings.TrimSpace(result.Stderr))
	}

	return nil
}

// GetAccessLog returns the last n lines of the Nginx access log.
func (s *Service) GetAccessLog(ctx context.Context, lines int) (string, error) {
	result, err := s.exec.RunSudo(ctx, "tail", "-n", strconv.Itoa(lines), "/var/log/nginx/access.log")
	if err != nil {
		return "", fmt.Errorf("read access log: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read access log: %s", strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// GetErrorLog returns the last n lines of the Nginx error log.
func (s *Service) GetErrorLog(ctx context.Context, lines int) (string, error) {
	result, err := s.exec.RunSudo(ctx, "tail", "-n", strconv.Itoa(lines), "/var/log/nginx/error.log")
	if err != nil {
		return "", fmt.Errorf("read error log: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read error log: %s", strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// validateSiteName checks that the site name does not contain path traversal.
func validateSiteName(name string) error {
	if name == "" {
		return model.NewValidationError("site name is required")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "..") {
		return model.NewValidationError("site name must not contain '/' or '..'")
	}
	return nil
}

// parseVersion extracts the version string from nginx -v stderr output.
// Input example: "nginx version: nginx/1.24.0"
func parseVersion(stderr string) string {
	const prefix = "nginx/"
	idx := strings.Index(stderr, prefix)
	if idx < 0 {
		return ""
	}
	version := stderr[idx+len(prefix):]
	// Trim any trailing whitespace or newlines.
	version = strings.TrimSpace(version)
	// Take only up to the first space (in case of extra info).
	if spaceIdx := strings.IndexByte(version, ' '); spaceIdx >= 0 {
		version = version[:spaceIdx]
	}
	return version
}

// parseProps parses key=value lines into a map.
func parseProps(output string) map[string]string {
	props := make(map[string]string)
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			props[parts[0]] = parts[1]
		}
	}
	return props
}

// splitLines splits a string by newlines and returns non-empty trimmed lines.
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
