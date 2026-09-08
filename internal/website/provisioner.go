package website

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	nginxconfig "github.com/mohammadirham37/jenderal_panel/internal/nginx"
	"github.com/mohammadirham37/jenderal_panel/internal/siteops"
)

// Provisioner handles background provisioning of websites.
type Provisioner struct {
	db            *sql.DB
	exec          executor.CommandExecutor
	audit         *audit.Service
	queue         chan string
	ipv6Available func() bool
	mutations     *siteops.Coordinator
}

// NewProvisioner creates a new Provisioner with a buffered queue channel.
func NewProvisioner(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Provisioner {
	return &Provisioner{
		db:            db,
		exec:          exec,
		audit:         auditSvc,
		queue:         make(chan string, 100),
		ipv6Available: nginxconfig.IPv6Available,
		mutations:     siteops.Default,
	}
}

// Start launches a goroutine that processes the provisioning queue.
// It blocks on the queue channel until the context is cancelled.
func (p *Provisioner) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case websiteID := <-p.queue:
				p.provision(ctx, websiteID)
			}
		}
	}()
}

// Queue enqueues a website ID for provisioning. It is non-blocking; if the
// queue is full the request is silently dropped.
func (p *Provisioner) Queue(websiteID string) {
	select {
	case p.queue <- websiteID:
	default:
	}
}

// provision executes the full provisioning pipeline for a website.
func (p *Provisioner) provision(ctx context.Context, websiteID string) {
	unlock := p.mutations.Lock(websiteID)
	defer unlock()

	// Load website from DB.
	w, err := p.loadWebsite(ctx, websiteID)
	if err != nil {
		p.logAudit(ctx, "provision_error", websiteID, "failed to load website: "+err.Error())
		return
	}

	homeDir := "/home/" + w.WebUser
	logDir := homeDir + "/logs"

	// Step 1: installing
	if err := p.updateStatus(ctx, websiteID, "installing", ""); err != nil {
		return
	}

	// Create system user.
	result, err := p.exec.RunSudo(ctx, "useradd",
		"--system",
		"--home-dir", homeDir,
		"--create-home",
		"--shell", "/usr/sbin/nologin",
		w.WebUser,
	)
	if err != nil {
		p.fail(ctx, websiteID, "create user failed: "+err.Error())
		return
	}
	// Exit code 9 means user already exists, which is acceptable.
	if result.ExitCode != 0 && result.ExitCode != 9 {
		p.fail(ctx, websiteID, "create user failed: "+strings.TrimSpace(result.Stderr))
		return
	}

	// Create directories.
	dirs := []string{
		w.DocumentRoot,
		logDir,
		homeDir + "/tmp",
	}
	for _, dir := range dirs {
		result, err = p.exec.RunSudo(ctx, "mkdir", "-p", dir)
		if err != nil {
			p.fail(ctx, websiteID, "create directory failed: "+err.Error())
			return
		}
		if result.ExitCode != 0 {
			p.fail(ctx, websiteID, "create directory failed: "+strings.TrimSpace(result.Stderr))
			return
		}
	}

	// Set ownership.
	result, err = p.exec.RunSudo(ctx, "chown", "-R", w.WebUser+":"+w.WebUser, homeDir)
	if err != nil {
		p.fail(ctx, websiteID, "chown failed: "+err.Error())
		return
	}
	if result.ExitCode != 0 {
		p.fail(ctx, websiteID, "chown failed: "+strings.TrimSpace(result.Stderr))
		return
	}

	// Step 2: configuring
	if err := p.updateStatus(ctx, websiteID, "configuring", ""); err != nil {
		return
	}

	// Build aliases from domains.
	domains, err := p.loadDomains(ctx, websiteID)
	if err != nil {
		p.fail(ctx, websiteID, "load domains failed: "+err.Error())
		return
	}
	var aliases []string
	for _, d := range domains {
		if d.Type != "primary" {
			aliases = append(aliases, d.Name)
		}
	}

	// Render and write nginx vhost config.
	vhostData := VhostData{
		Domain:            w.Domain,
		Aliases:           strings.Join(aliases, " "),
		DocumentRoot:      w.DocumentRoot,
		ACMEChallengeRoot: DefaultACMEChallengeRoot,
		LogDir:            logDir,
		PHPVersion:        w.PHPVersion,
		AppType:           w.AppType,
		IPv6:              p.ipv6Available(),
	}

	vhostContent, err := RenderVhost(vhostData)
	if err != nil {
		p.fail(ctx, websiteID, "render vhost failed: "+err.Error())
		return
	}

	confPath := "/etc/nginx/sites-available/" + w.Domain
	enabledPath := "/etc/nginx/sites-enabled/" + w.Domain

	if err := p.writeSystemFile(ctx, vhostContent, confPath); err != nil {
		p.fail(ctx, websiteID, "write vhost config failed: "+err.Error())
		return
	}

	// Create symlink in sites-enabled.
	result, err = p.exec.RunSudo(ctx, "ln", "-sf", confPath, enabledPath)
	if err != nil {
		p.fail(ctx, websiteID, "enable site failed: "+err.Error())
		return
	}
	if result.ExitCode != 0 {
		p.fail(ctx, websiteID, "enable site failed: "+strings.TrimSpace(result.Stderr))
		return
	}

	// Render and write PHP-FPM pool config (if not static).
	if w.AppType != "static" && w.PHPVersion != "" {
		poolData := PoolData{
			Domain:     w.Domain,
			WebUser:    w.WebUser,
			PHPVersion: w.PHPVersion,
			HomeDir:    homeDir,
			LogDir:     logDir,
		}

		poolContent, err := RenderPool(poolData)
		if err != nil {
			p.fail(ctx, websiteID, "render pool failed: "+err.Error())
			return
		}

		poolPath := "/etc/php/" + w.PHPVersion + "/fpm/pool.d/" + w.Domain + ".conf"
		if err := p.writeSystemFile(ctx, poolContent, poolPath); err != nil {
			p.fail(ctx, websiteID, "write pool config failed: "+err.Error())
			return
		}
	}

	// Step 3: validating
	if err := p.updateStatus(ctx, websiteID, "validating", ""); err != nil {
		return
	}

	// Validate nginx config.
	result, err = p.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		p.rollbackConfigs(ctx, w)
		p.fail(ctx, websiteID, "nginx config test error: "+err.Error())
		return
	}
	if result.ExitCode != 0 {
		p.rollbackConfigs(ctx, w)
		p.fail(ctx, websiteID, "nginx config test failed: "+strings.TrimSpace(result.Stderr))
		return
	}

	// Reload nginx.
	result, err = p.exec.RunSudo(ctx, "systemctl", "reload", "nginx")
	if err != nil {
		p.fail(ctx, websiteID, "reload nginx failed: "+err.Error())
		return
	}
	if result.ExitCode != 0 {
		p.fail(ctx, websiteID, "reload nginx failed: "+strings.TrimSpace(result.Stderr))
		return
	}

	// Restart PHP-FPM if applicable.
	if w.AppType != "static" && w.PHPVersion != "" {
		fpmService := "php" + w.PHPVersion + "-fpm"
		result, err = p.exec.RunSudo(ctx, "systemctl", "restart", fpmService)
		if err != nil {
			p.fail(ctx, websiteID, "restart fpm failed: "+err.Error())
			return
		}
		if result.ExitCode != 0 {
			p.fail(ctx, websiteID, "restart fpm failed: "+strings.TrimSpace(result.Stderr))
			return
		}
	}

	// Step 4: active
	_ = p.updateStatus(ctx, websiteID, "active", "")
	p.logAudit(ctx, "website_provisioned", websiteID, "provisioned website "+w.Domain)
}

// writeSystemFile writes content to a temporary file and copies it to the
// target path using sudo.
func (p *Provisioner) writeSystemFile(ctx context.Context, content, targetPath string) error {
	tmpPath := "/tmp/jenderal_prov_" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	defer os.Remove(tmpPath)

	result, err := p.exec.RunSudo(ctx, "cp", tmpPath, targetPath)
	if err != nil {
		return fmt.Errorf("copy to %s: %w", targetPath, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("copy to %s: %s", targetPath, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// rollbackConfigs removes the nginx and fpm configs that were written during
// provisioning.
func (p *Provisioner) rollbackConfigs(ctx context.Context, w websiteRow) {
	confPath := "/etc/nginx/sites-available/" + w.Domain
	enabledPath := "/etc/nginx/sites-enabled/" + w.Domain
	_, _ = p.exec.RunSudo(ctx, "rm", "-f", confPath)
	_, _ = p.exec.RunSudo(ctx, "rm", "-f", enabledPath)

	if w.AppType != "static" && w.PHPVersion != "" {
		poolPath := "/etc/php/" + w.PHPVersion + "/fpm/pool.d/" + w.Domain + ".conf"
		_, _ = p.exec.RunSudo(ctx, "rm", "-f", poolPath)
	}
}

// fail sets the website status to failed with the given error message.
func (p *Provisioner) fail(ctx context.Context, websiteID, errMsg string) {
	_ = p.updateStatus(ctx, websiteID, "failed", errMsg)
	p.logAudit(ctx, "provision_failed", websiteID, errMsg)
}

// updateStatus updates the status and optional error_message of a website.
func (p *Provisioner) updateStatus(ctx context.Context, websiteID, status, errorMessage string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var err error
	if errorMessage != "" {
		_, err = p.db.ExecContext(ctx,
			`UPDATE websites SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`,
			status, errorMessage, now, websiteID,
		)
	} else {
		_, err = p.db.ExecContext(ctx,
			`UPDATE websites SET status = ?, error_message = NULL, updated_at = ? WHERE id = ?`,
			status, now, websiteID,
		)
	}
	if err != nil {
		return fmt.Errorf("update status to %s: %w", status, err)
	}
	return nil
}

// websiteRow holds the fields needed during provisioning.
type websiteRow struct {
	ID           string
	Domain       string
	AppType      string
	PHPVersion   string
	DocumentRoot string
	WebUser      string
}

// domainRow holds the fields of a domain for provisioning.
type domainRow struct {
	Name string
	Type string
}

// loadWebsite loads the minimal website fields needed for provisioning.
func (p *Provisioner) loadWebsite(ctx context.Context, id string) (websiteRow, error) {
	var w websiteRow
	var phpVersion sql.NullString
	err := p.db.QueryRowContext(ctx,
		`SELECT id, domain, app_type, php_version, document_root, web_user
		 FROM websites WHERE id = ?`, id,
	).Scan(&w.ID, &w.Domain, &w.AppType, &phpVersion, &w.DocumentRoot, &w.WebUser)
	if err != nil {
		return w, err
	}
	w.PHPVersion = phpVersion.String
	return w, nil
}

// loadDomains loads all domains for a website.
func (p *Provisioner) loadDomains(ctx context.Context, websiteID string) ([]domainRow, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT name, type FROM domains WHERE website_id = ?`, websiteID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []domainRow
	for rows.Next() {
		var d domainRow
		if err := rows.Scan(&d.Name, &d.Type); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	return domains, rows.Err()
}

// logAudit writes an audit entry for provisioning events.
func (p *Provisioner) logAudit(ctx context.Context, action, target, detail string) {
	if p.audit == nil {
		return
	}
	_ = p.audit.Log(ctx, audit.LogEntry{
		Action: action,
		Module: "website",
		Target: target,
		Detail: detail,
	})
}
