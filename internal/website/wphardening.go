package website

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// wpHardeningDir holds one nginx include per WordPress site. The wordpress
// vhost profile includes it through a glob, so a missing file is harmless.
const wpHardeningDir = "/etc/nginx/jenderal/wordpress"

// WpHardening is the set of security hardening toggles for a WordPress site.
// The first five are nginx rules, the next three wp-config.php constants,
// and the last a file mode on wp-config.php.
type WpHardening struct {
	BlockUploadPHP   bool   `json:"block_upload_php"`
	BlockIncludesPHP bool   `json:"block_includes_php"`
	BlockSensitive   bool   `json:"block_sensitive"`
	BlockAuthorScan  bool   `json:"block_author_scan"`
	SecurityHeaders  bool   `json:"security_headers"`
	DisallowFileEdit bool   `json:"disallow_file_edit"`
	ForceSSLAdmin    bool   `json:"force_ssl_admin"`
	DisableDebug     bool   `json:"disable_debug"`
	ProtectWpConfig  bool   `json:"protect_wp_config"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

// wpHardeningPath is the nginx include holding the site's hardening rules.
func wpHardeningPath(domain string) string {
	return filepath.Join(wpHardeningDir, domain+".conf")
}

// wpHardeningIncludeGlob is what the wordpress vhost profile includes. The
// trailing glob makes nginx accept the include while the file is absent.
func wpHardeningIncludeGlob(domain string) string {
	return wpHardeningPath(domain) + "*"
}

// renderWpHardening renders the nginx rules for the enabled toggles. The
// include sits before the vhost's generic PHP location, so its regex
// locations win for the paths they match.
func renderWpHardening(h WpHardening) string {
	var b strings.Builder
	b.WriteString("# Managed by Jenderal Panel (WordPress hardening). Do not edit.\n")
	if h.BlockUploadPHP {
		b.WriteString(`
# Never execute PHP dropped into the uploads directory.
location ~* ^/wp-content/uploads/.*\.(php|phtml|phar)$ {
    deny all;
    access_log off;
}
`)
	}
	if h.BlockIncludesPHP {
		b.WriteString(`
# Core include files are only meant to be loaded by WordPress itself.
location ~* ^/wp-admin/includes/ {
    deny all;
}
location ~* ^/wp-includes/[^/]+\.php$ {
    deny all;
}
location ~* ^/wp-includes/js/tinymce/langs/.+\.php$ {
    deny all;
}
location ~* ^/wp-includes/theme-compat/ {
    deny all;
}
`)
	}
	if h.BlockSensitive {
		b.WriteString(`
# Configuration, version disclosure, logs, dumps, and backups.
location ~* /(wp-config\.php|wp-config-sample\.php|readme\.html|license\.txt)$ {
    deny all;
    access_log off;
    log_not_found off;
}
location ~* \.(log|sql|sqlite|bak|old|orig|swp|dist)$ {
    deny all;
    access_log off;
    log_not_found off;
}
`)
	}
	if h.BlockAuthorScan {
		b.WriteString(`
# Stop user enumeration through /?author=N (wp-admin filters by author too).
set $jenderal_wp_author_scan 0;
if ($args ~* "(^|&)author=\d+") {
    set $jenderal_wp_author_scan 1;
}
if ($uri ~* "^/wp-admin/") {
    set $jenderal_wp_author_scan 0;
}
if ($jenderal_wp_author_scan = 1) {
    return 403;
}
`)
	}
	if h.SecurityHeaders {
		b.WriteString(`
add_header X-Frame-Options SAMEORIGIN always;
add_header X-Content-Type-Options nosniff always;
add_header Referrer-Policy strict-origin-when-cross-origin always;
add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;
`)
	}
	return b.String()
}

// GetWpHardening returns the stored hardening toggles (all off when the site
// was never hardened).
func (s *Service) GetWpHardening(ctx context.Context, websiteID string) (WpHardening, error) {
	if _, _, err := s.wpContextForID(ctx, websiteID); err != nil {
		return WpHardening{}, err
	}
	return s.loadWpHardening(ctx, websiteID)
}

func (s *Service) loadWpHardening(ctx context.Context, websiteID string) (WpHardening, error) {
	var h WpHardening
	err := s.db.QueryRowContext(ctx, `SELECT block_upload_php, block_includes_php, block_sensitive,
		block_author_scan, security_headers, disallow_file_edit, force_ssl_admin, disable_debug,
		protect_wp_config, updated_at FROM website_wp_hardening WHERE website_id = ?`, websiteID).
		Scan(&h.BlockUploadPHP, &h.BlockIncludesPHP, &h.BlockSensitive, &h.BlockAuthorScan,
			&h.SecurityHeaders, &h.DisallowFileEdit, &h.ForceSSLAdmin, &h.DisableDebug,
			&h.ProtectWpConfig, &h.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WpHardening{}, nil
	}
	if err != nil {
		return WpHardening{}, fmt.Errorf("load wordpress hardening: %w", err)
	}
	return h, nil
}

// ApplyWpHardening writes the nginx rules, sets the wp-config.php constants,
// adjusts the wp-config.php mode, and stores the toggles. Turning a toggle
// off reverts what it controls, except DisableDebug: switching it off leaves
// the debug constants as they are rather than re-enabling debug output.
func (s *Service) ApplyWpHardening(ctx context.Context, websiteID string, h WpHardening) (WpHardening, error) {
	wpCtx, w, err := s.wpContextForID(ctx, websiteID)
	if err != nil {
		return WpHardening{}, err
	}
	if !safeDomainComponent(w.Domain) {
		return WpHardening{}, fmt.Errorf("unsafe website domain %q", w.Domain)
	}
	if !s.hasProjectFile(ctx, wpCtx.path, "wp-config.php") {
		return WpHardening{}, model.NewValidationError("WordPress is not installed yet (wp-config.php not found)")
	}
	if h.ForceSSLAdmin {
		sslDomains, err := s.activeSSLDomains(ctx, websiteID)
		if err != nil {
			return WpHardening{}, err
		}
		if len(sslDomains) == 0 {
			return WpHardening{}, model.NewValidationError("forcing HTTPS for wp-admin needs an active SSL certificate on this site")
		}
	}

	if err := s.applyWpHardeningNginx(ctx, w, h); err != nil {
		return WpHardening{}, err
	}

	constants := []struct {
		name  string
		value bool
		apply bool
	}{
		{"DISALLOW_FILE_EDIT", h.DisallowFileEdit, true},
		{"FORCE_SSL_ADMIN", h.ForceSSLAdmin, true},
		{"WP_DEBUG", false, h.DisableDebug},
		{"WP_DEBUG_DISPLAY", false, h.DisableDebug},
	}
	for _, c := range constants {
		if !c.apply {
			continue
		}
		value := "false"
		if c.value {
			value = "true"
		}
		if _, err := s.runWp(ctx, wpCtx, w.WebUser, "config", "set", c.name, value, "--raw", "--type=constant"); err != nil {
			return WpHardening{}, model.NewDomainError("WP_CONFIG_ERROR", "failed to set "+c.name, err)
		}
	}

	if err := s.setWpConfigMode(ctx, wpCtx, w, h.ProtectWpConfig); err != nil {
		return WpHardening{}, err
	}

	h.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO website_wp_hardening (website_id, block_upload_php,
		block_includes_php, block_sensitive, block_author_scan, security_headers, disallow_file_edit,
		force_ssl_admin, disable_debug, protect_wp_config, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(website_id) DO UPDATE SET block_upload_php = excluded.block_upload_php,
			block_includes_php = excluded.block_includes_php, block_sensitive = excluded.block_sensitive,
			block_author_scan = excluded.block_author_scan, security_headers = excluded.security_headers,
			disallow_file_edit = excluded.disallow_file_edit, force_ssl_admin = excluded.force_ssl_admin,
			disable_debug = excluded.disable_debug, protect_wp_config = excluded.protect_wp_config,
			updated_at = excluded.updated_at`,
		websiteID, boolToInt(h.BlockUploadPHP), boolToInt(h.BlockIncludesPHP), boolToInt(h.BlockSensitive),
		boolToInt(h.BlockAuthorScan), boolToInt(h.SecurityHeaders), boolToInt(h.DisallowFileEdit),
		boolToInt(h.ForceSSLAdmin), boolToInt(h.DisableDebug), boolToInt(h.ProtectWpConfig), h.UpdatedAt,
	); err != nil {
		return WpHardening{}, fmt.Errorf("save wordpress hardening: %w", err)
	}
	return h, nil
}

// applyWpHardeningNginx installs the rules file, re-renders the vhosts so
// they carry the include, and validates nginx. An invalid result restores
// the previous rules and reloads, leaving the site as it was.
func (s *Service) applyWpHardeningNginx(ctx context.Context, w model.Website, h WpHardening) error {
	path := wpHardeningPath(w.Domain)
	previous := ""
	if res, err := s.exec.RunSudo(ctx, "cat", path); err == nil && res.ExitCode == 0 {
		previous = res.Stdout
	}

	if err := s.installRootFile(ctx, renderWpHardening(h), path); err != nil {
		return fmt.Errorf("write wordpress hardening rules: %w", err)
	}
	if err := s.regenerateConfig(ctx, w, ""); err != nil {
		return fmt.Errorf("regenerate vhost: %w", err)
	}

	test, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return fmt.Errorf("test nginx config: %w", err)
	}
	if test.ExitCode != 0 {
		if previous != "" {
			_ = s.installRootFile(ctx, previous, path)
		} else {
			_ = s.runSudoOK(ctx, "rm", "-f", path)
		}
		_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")
		return model.NewDomainError("NGINX_CONFIG_INVALID",
			"nginx rejected the hardening rules: "+strings.TrimSpace(test.Stderr), nil)
	}
	return nil
}

// installRootFile writes content to a root-owned 0644 file, creating parent
// directories as needed.
func (s *Service) installRootFile(ctx context.Context, content, dest string) error {
	tmp, err := os.CreateTemp("", "jenderal_wp_hardening_*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return s.runSudoOK(ctx, "install", "-D", "-m", "0644", "-o", "root", "-g", "root", tmpPath, dest)
}

// setWpConfigMode makes wp-config.php owner-only (0600) or restores the
// default 0644. PHP-FPM runs as the site user, so 0600 keeps the site working
// while hiding database credentials from other local users.
func (s *Service) setWpConfigMode(ctx context.Context, wpCtx wpContext, w model.Website, protect bool) error {
	out, err := s.runWp(ctx, wpCtx, w.WebUser, "config", "path")
	if err != nil {
		return model.NewDomainError("WP_CONFIG_ERROR", "failed to locate wp-config.php", err)
	}
	configPath := filepath.Clean(strings.TrimSpace(out))
	home := filepath.Join("/home", w.WebUser) + string(filepath.Separator)
	if filepath.Base(configPath) != "wp-config.php" || !strings.HasPrefix(configPath, home) {
		return model.NewValidationError("wp-config.php is outside the site home directory")
	}
	mode := "0644"
	if protect {
		mode = "0600"
	}
	res, err := s.exec.RunSudo(ctx, "-u", w.WebUser, "--", "chmod", mode, "--", configPath)
	if err != nil {
		return fmt.Errorf("chmod wp-config.php: %w", err)
	}
	if res.ExitCode != 0 {
		return model.NewDomainError("FILE_ERROR", "failed to chmod wp-config.php: "+strings.TrimSpace(res.Stderr), nil)
	}
	return nil
}
