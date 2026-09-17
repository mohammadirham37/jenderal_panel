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
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode"`
	RulesReady bool   `json:"rules_ready"`
}

// DoSStatus reports the mod_evasive-style rate limiting state.
type DoSStatus struct {
	Enabled           bool `json:"enabled"`
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
	status.ModSecurity.Enabled = s.fileExists(ctx, enableFile)

	if content, ok := s.readFile(ctx, dosFile); ok {
		status.DoS.Enabled = true
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

// Enable turns ModSecurity on for every nginx website via a conf.d include.
func (s *Service) Enable(ctx context.Context) error {
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
	content := "# Managed by Jenderal Panel - ModSecurity WAF\n" +
		"modsecurity on;\n" +
		"modsecurity_rules_file " + rulesFile + ";\n"
	if err := s.writeFile(ctx, enableFile, content); err != nil {
		return fmt.Errorf("enable WAF: %w", err)
	}
	if err := s.reloadNginx(ctx); err != nil {
		s.rollbackFile(ctx, enableFile)
		return err
	}
	return nil
}

// Disable turns ModSecurity off while keeping the installation in place.
func (s *Service) Disable(ctx context.Context) error {
	if !s.fileExists(ctx, enableFile) {
		return nil
	}
	if _, err := s.exec.RunSudo(ctx, "rm", "-f", enableFile); err != nil {
		return fmt.Errorf("disable WAF: %w", err)
	}
	if err := s.reloadNginx(ctx); err != nil {
		return err
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

// ConfigureDoS enables (or updates) the mod_evasive-style per-IP rate limit:
// clients exceeding requests_per_minute (with the given burst) receive 403.
func (s *Service) ConfigureDoS(ctx context.Context, requestsPerMinute, burst int) error {
	if requestsPerMinute < 10 || requestsPerMinute > 60000 {
		return model.NewValidationError("requests_per_minute must be between 10 and 60000")
	}
	if burst < 1 || burst > 1000 {
		return model.NewValidationError("burst must be between 1 and 1000")
	}
	content := fmt.Sprintf(`# Managed by Jenderal Panel - DoS protection (mod_evasive-style rate limiting)
limit_req_zone $binary_remote_addr zone=jenderal_dos:10m rate=%dr/m;
limit_req_status 403;
limit_req zone=jenderal_dos burst=%d nodelay;
`, requestsPerMinute, burst)
	if err := s.writeFile(ctx, dosFile, content); err != nil {
		return fmt.Errorf("enable DoS protection: %w", err)
	}
	if err := s.reloadNginx(ctx); err != nil {
		s.rollbackFile(ctx, dosFile)
		return err
	}
	return nil
}

// DisableDoS turns the per-IP rate limit off.
func (s *Service) DisableDoS(ctx context.Context) error {
	if !s.fileExists(ctx, dosFile) {
		return nil
	}
	if _, err := s.exec.RunSudo(ctx, "rm", "-f", dosFile); err != nil {
		return fmt.Errorf("disable DoS protection: %w", err)
	}
	if err := s.reloadNginx(ctx); err != nil {
		return err
	}
	return nil
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
