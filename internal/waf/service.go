// Package waf manages the nginx web application firewall (ModSecurity with
// the OWASP Core Rule Set) and the mod_evasive-style DoS protection: global
// per-IP request rate limiting that answers 403 once a client exceeds the
// configured threshold.
package waf

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const (
	connectorPackage = "libnginx-mod-http-modsecurity"
	crsPackage       = "modsecurity-crs"
	coreConfig       = "/etc/modsecurity/modsecurity.conf"
	crsSetupExample  = "/usr/share/modsecurity-crs/crs-setup.conf.example"
	crsSetup         = "/usr/share/modsecurity-crs/crs-setup.conf"
	crsSetupManaged  = "/etc/nginx/jenderal-crs-setup.conf"
	crsRulesDir      = "/usr/share/modsecurity-crs/rules"
	rulesFile        = "/etc/nginx/jenderal-modsecurity-rules.conf"
	auditLog         = "/var/log/nginx/modsec_audit.log"
	enableFile       = "/etc/nginx/conf.d/jenderal-modsecurity.conf"
	dosFile          = "/etc/nginx/conf.d/jenderal-dosevasive.conf"
)

// Mode selects how ModSecurity treats rule matches.
const (
	ModeBlocking      = "blocking"
	ModeDetectionOnly = "detection_only"
)

// ModSecurityStatus reports the nginx WAF state.
type ModSecurityStatus struct {
	Installed  bool   `json:"installed"`
	Mode       string `json:"mode"`
	RulesReady bool   `json:"rules_ready"`
}

// DoSStatus reports the mod_evasive-style rate limiting state.
type DoSStatus struct {
	Defaults          bool `json:"defaults"`
	RequestsPerMinute int  `json:"requests_per_minute"`
	Burst             int  `json:"burst"`
}

// Status is the payload behind GET /security/waf.
type Status struct {
	ModSecurity ModSecurityStatus `json:"modsecurity"`
	DoS         DoSStatus         `json:"dosevasive"`
}

// Service manages the WAF and DoS protection configuration.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new waf Service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// Status reports both protections' current state.
func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{
		ModSecurity: ModSecurityStatus{Mode: ""},
		DoS:         DoSStatus{RequestsPerMinute: 120, Burst: 20},
	}

	installed := 0
	for _, pkg := range []string{connectorPackage, crsPackage} {
		ok, err := s.packageInstalled(ctx, pkg)
		if err != nil {
			return status, err
		}
		if ok {
			installed++
		}
	}
	status.ModSecurity.Installed = installed == 2


	if content, ok := s.readFile(ctx, rulesFile); ok {
		status.ModSecurity.RulesReady = true
		switch {
		case strings.Contains(content, "SecRuleEngine DetectionOnly"):
			status.ModSecurity.Mode = ModeDetectionOnly
		case strings.Contains(content, "SecRuleEngine On"):
			status.ModSecurity.Mode = ModeBlocking
		}
	}
	// Legacy migration: the old global include filtered every server block
	// (panel included); protection is now per-site. Removing it happens on
	// the first status read after the update.
	if s.fileExists(ctx, enableFile) {
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", enableFile)
		_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")
	}
	s.migrateLegacyDoSConf(ctx)

	if content, ok := s.readFile(ctx, dosFile); ok {
		status.DoS.Defaults = true
		if match := regexp.MustCompile(`rate=(\d+)r/m`).FindStringSubmatch(content); match != nil {
			if rate, err := strconv.Atoi(match[1]); err == nil {
				status.DoS.RequestsPerMinute = rate
			}
		}
		if match := regexp.MustCompile(`burst=(\d+)`).FindStringSubmatch(content); match != nil {
			if burst, err := strconv.Atoi(match[1]); err == nil {
				status.DoS.Burst = burst
			}
		}
	}
	return status, nil
}

// Install installs the ModSecurity nginx connector and the OWASP Core Rule
// Set, then prepares the panel-managed rules include.
func (s *Service) Install(ctx context.Context, log func(string)) error {
	if log != nil {
		log("Updating package index...")
	}
	if err := s.runSudoOK(ctx, "apt-get", "update", "-qq"); err != nil {
		return fmt.Errorf("apt update: %w", err)
	}
	if log != nil {
		log("Installing ModSecurity nginx connector and OWASP CRS...")
	}
	if err := s.runSudoOK(ctx, "apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120",
		connectorPackage, crsPackage); err != nil {
		return fmt.Errorf("install waf packages: %w", err)
	}
	if err := s.writeRulesFile(ctx, ModeBlocking); err != nil {
		return err
	}
	if err := s.prepareAuditLog(ctx); err != nil {
		return err
	}
	if log != nil {
		log("WAF installed. Enable it to protect every nginx website.")
	}
	return nil
}

// SiteProtection is the per-site enforcement state, read from the site's
// security include file (/etc/nginx/jenderal/security/sites/<id>.conf).
type SiteProtection struct {
	WAF bool `json:"waf"`
	DoS bool `json:"dos"`
}

const siteHookFormat = "/etc/nginx/jenderal/security/sites/%s.conf"

// siteHookPath validates the site id and returns its security include path.
func siteHookPath(siteID string) (string, error) {
	if len(siteID) < 10 || len(siteID) > 64 {
		return "", model.NewValidationError("invalid site id")
	}
	for _, r := range siteID {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return "", model.NewValidationError("invalid site id")
		}
	}
	return fmt.Sprintf(siteHookFormat, siteID), nil
}

// siteProtectionFromHook parses the include file into the per-site state.
func siteProtectionFromHook(content string) SiteProtection {
	return SiteProtection{
		WAF: strings.Contains(content, "modsecurity on;"),
		DoS: strings.Contains(content, "limit_req zone=jenderal_dos"),
	}
}

// GetSiteProtection reports the per-site WAF/DoS enforcement state.
func (s *Service) GetSiteProtection(ctx context.Context, siteID string) (SiteProtection, error) {
	hookPath, err := siteHookPath(siteID)
	if err != nil {
		return SiteProtection{}, err
	}
	content, ok := s.readFile(ctx, hookPath)
	if !ok {
		return SiteProtection{WAF: false, DoS: false}, nil
	}
	return siteProtectionFromHook(content), nil
}

// SetSiteProtection turns WAF and/or DoS enforcement on or off for ONE
// website by writing its security include file (already included by the
// site's vhost) and reloading nginx. The panel domain has no such include
// and is therefore never filtered.
func (s *Service) SetSiteProtection(ctx context.Context, siteID string, protect SiteProtection, dosRate, dosBurst int) error {
	hookPath, err := siteHookPath(siteID)
	if err != nil {
		return err
	}

	var parts []string
	if protect.WAF {
		if err := s.requireInstalled(ctx); err != nil {
			return err
		}
		mode := ModeBlocking
		if content, ok := s.readFile(ctx, rulesFile); ok && strings.Contains(content, "SecRuleEngine DetectionOnly") {
			mode = ModeDetectionOnly
		}
		if err := s.writeRulesFile(ctx, mode); err != nil {
			return err
		}
		if err := s.prepareAuditLog(ctx); err != nil {
			return err
		}
		parts = append(parts, "modsecurity on;\nmodsecurity_rules_file "+rulesFile+";\n")
	}
	if protect.DoS {
		if err := s.writeDoSZone(ctx, dosRate); err != nil {
			return err
		}
		parts = append(parts, "limit_req_status 403;\nlimit_req zone=jenderal_dos burst="+strconv.Itoa(dosBurst)+" nodelay;\n")
	}

	previous := ""
	if content, ok := s.readFile(ctx, hookPath); ok {
		previous = content
	}

	var content string
	if len(parts) > 0 {
		content = "# Managed by Jenderal Panel - per-site protection\n" + strings.Join(parts, "")
		if err := s.writeFile(ctx, hookPath, content); err != nil {
			return fmt.Errorf("enable site protection: %w", err)
		}
	} else {
		// Both off: remove the include entirely.
		if _, err := s.exec.RunSudo(ctx, "rm", "-f", hookPath); err != nil {
			return fmt.Errorf("disable site protection: %w", err)
		}
	}

	if err := s.reloadNginx(ctx); err != nil {
		s.rollbackFile(ctx, hookPath)
		if previous != "" {
			if writeErr := s.writeFile(ctx, hookPath, previous); writeErr != nil {
				return fmt.Errorf("rollback hook: %w (original error: %v)", writeErr, err)
			}
			if reloadErr := s.reloadNginx(ctx); reloadErr != nil {
				return fmt.Errorf("rollback reload: %w (original error: %v)", reloadErr, err)
			}
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("reload nginx: site protection change rolled back")
	}
	return nil
}

// SetMode switches between blocking and detection-only operation.
func (s *Service) SetMode(ctx context.Context, mode string) error {
	if mode != ModeBlocking && mode != ModeDetectionOnly {
		return model.NewValidationError("mode must be blocking or detection_only")
	}
	if err := s.requireInstalled(ctx); err != nil {
		return err
	}
	if err := s.writeRulesFile(ctx, mode); err != nil {
		return err
	}
	return s.prepareAuditLog(ctx)
}

func (s *Service) writeRulesFile(ctx context.Context, mode string) error {
	var b strings.Builder
	b.WriteString("# Managed by Jenderal Panel - ModSecurity + OWASP Core Rule Set\n")
	if s.fileExists(ctx, coreConfig) {
		b.WriteString("Include " + coreConfig + "\n")
	}
	// The shipped crs-setup.conf.example is fully commented out; including a
	// copy as-is leaves tx.crs_setup_version unset and CRS rule 901001 then
	// denies every request with 500. Serve our own active initialization.
	if err := s.writeFile(ctx, crsSetupManaged,
		"# Managed by Jenderal Panel - active CRS initialization\n"+
			"SecAction \"id:900100, phase:1, nolog, pass, t:none, setvar:tx.crs_setup_version=335\"\n"); err != nil {
		return fmt.Errorf("write CRS setup: %w", err)
	}
	b.WriteString("Include " + crsSetupManaged + "\n")
	if s.fileExists(ctx, crsRulesDir) {
		b.WriteString("Include " + crsRulesDir + "/*.conf\n")
	}
	if mode == ModeDetectionOnly {
		b.WriteString("SecRuleEngine DetectionOnly\n")
	} else {
		b.WriteString("SecRuleEngine On\n")
	}
	// The stock /etc/modsecurity/modsecurity.conf (an Apache-flavored
	// package) points SecAuditLog at /var/log/apache2, which the nginx
	// worker cannot write to — every proxied request then fails with 500.
	// Re-anchor the logs and temp dirs after the includes so ours win.
	b.WriteString("# Keep ModSecurity state where the nginx worker can write.\n")
	b.WriteString("SecAuditLog " + auditLog + "\n")
	b.WriteString("SecTmpDir /tmp\n")
	b.WriteString("SecDataDir /tmp\n")
	if err := s.writeFile(ctx, rulesFile, b.String()); err != nil {
		return fmt.Errorf("write WAF rules: %w", err)
	}
	return nil
}

// prepareAuditLog makes sure the audit log exists and is writable by the
// nginx worker user before ModSecurity tries to open it.
func (s *Service) prepareAuditLog(ctx context.Context) error {
	if err := s.runSudoOK(ctx, "touch", auditLog); err != nil {
		return fmt.Errorf("create modsecurity audit log: %w", err)
	}
	if err := s.runSudoOK(ctx, "chown", "www-data:adm", auditLog); err != nil {
		return fmt.Errorf("chown modsecurity audit log: %w", err)
	}
	return s.runSudoOK(ctx, "chmod", "0660", auditLog)
}

// SetDoSDefaults writes the shared DoS rate-limit zone used by the per-site
// security includes. Enforcement itself is opt-in per website.
func (s *Service) SetDoSDefaults(ctx context.Context, requestsPerMinute, burst int) error {
	if requestsPerMinute < 10 || requestsPerMinute > 60000 {
		return model.NewValidationError("requests_per_minute must be between 10 and 60000")
	}
	if burst < 1 || burst > 1000 {
		return model.NewValidationError("burst must be between 1 and 1000")
	}
	if err := s.writeDoSZone(ctx, requestsPerMinute); err != nil {
		return err
	}
	return s.reloadNginx(ctx)
}

// writeDoSZone (re)writes the shared rate-limit zone definition without
// reloading nginx.
func (s *Service) writeDoSZone(ctx context.Context, requestsPerMinute int) error {
	content := fmt.Sprintf(`# Managed by Jenderal Panel - DoS protection rate limit zone
limit_req_zone $binary_remote_addr zone=jenderal_dos:10m rate=%dr/m;
`, requestsPerMinute)
	if err := s.writeFile(ctx, dosFile, content); err != nil {
		return fmt.Errorf("write DoS zone: %w", err)
	}
	return nil
}

// legacyDoSGlobalLimit detects the pre-per-site conf that rate-limited
// every server (including the panel) and migrates it to the zone-only form.
func (s *Service) migrateLegacyDoSConf(ctx context.Context) {
	content, ok := s.readFile(ctx, dosFile)
	if !ok {
		return
	}
	if !strings.Contains(content, "\nlimit_req zone=") && !strings.HasPrefix(content, "limit_req zone=") {
		return
	}
	var zone []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "limit_req_zone ") || strings.Contains(line, "Managed by Jenderal Panel") {
			zone = append(zone, line)
		}
	}
	if len(zone) > 0 {
		_ = s.writeFile(ctx, dosFile, strings.Join(zone, "\n")+"\n")
	}
}

func (s *Service) requireInstalled(ctx context.Context) error {
	if !s.fileExists(ctx, rulesFile) && !s.packageDirReady(ctx) {
		return model.NewValidationError("install ModSecurity first")
	}
	return nil
}

func (s *Service) packageDirReady(ctx context.Context) bool {
	return s.fileExists(ctx, crsRulesDir)
}

func (s *Service) packageInstalled(ctx context.Context, pkg string) (bool, error) {
	result, err := s.exec.RunSudo(ctx, "dpkg-query", "-W", "-f=${Status}", pkg)
	if err != nil {
		return false, fmt.Errorf("check package %s: %w", pkg, err)
	}
	if result == nil {
		return false, errors.New("executor returned no result")
	}
	return result.ExitCode == 0 && strings.Contains(result.Stdout, "install ok installed"), nil
}

func (s *Service) fileExists(ctx context.Context, path string) bool {
	result, err := s.exec.RunSudo(ctx, "test", "-e", path)
	if err != nil || result == nil {
		return false
	}
	return result.ExitCode == 0
}

func (s *Service) readFile(ctx context.Context, path string) (string, bool) {
	result, err := s.exec.RunSudo(ctx, "cat", path)
	if err != nil || result == nil || result.ExitCode != 0 {
		return "", false
	}
	return result.Stdout, true
}

func (s *Service) writeFile(ctx context.Context, path, content string) error {
	result, err := s.exec.RunSudoWithInput(ctx, content, "tee", path)
	if err != nil {
		return err
	}
	if result == nil || result.ExitCode != 0 {
		detail := "exit status non-zero"
		if result != nil && strings.TrimSpace(result.Stderr) != "" {
			detail = strings.TrimSpace(result.Stderr)
		}
		return fmt.Errorf("write %s: %s", path, detail)
	}
	return nil
}

func (s *Service) rollbackFile(ctx context.Context, path string) {
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", path)
}

func (s *Service) runSudoOK(ctx context.Context, name string, args ...string) error {
	result, err := s.exec.RunSudo(ctx, name, args...)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("executor returned no result")
	}
	if result.ExitCode != 0 {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = fmt.Sprintf("exit status %d", result.ExitCode)
		}
		return fmt.Errorf("%s: %s", name, detail)
	}
	return nil
}

// reloadNginx validates the configuration first so a broken state never
// reaches the running server.
func (s *Service) reloadNginx(ctx context.Context) error {
	if err := s.runSudoOK(ctx, "/usr/sbin/nginx", "-t"); err != nil {
		return fmt.Errorf("nginx config test failed: %w", err)
	}
	return s.runSudoOK(ctx, "/usr/bin/systemctl", "reload", "nginx")
}
