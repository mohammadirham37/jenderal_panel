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

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/landing"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	nginxconfig "github.com/mohammadirham37/jenderal_panel/internal/nginx"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
	"github.com/mohammadirham37/jenderal_panel/internal/siteops"
	"github.com/oklog/ulid/v2"
)

// Provisioner handles background provisioning of websites.
type Provisioner struct {
	db            *sql.DB
	exec          executor.CommandExecutor
	audit         *audit.Service
	queue         chan string
	ipv6Available func() bool
	mutations     *siteops.Coordinator
	installer     *Installer
	runtime       *noderuntime.Service
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
		installer:     NewInstaller(exec),
		runtime:       noderuntime.New(exec),
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

// Queue enqueues a website ID for provisioning and reports when the caller
// stops waiting for queue capacity. This prevents accepted work from being
// silently lost while the worker is busy.
func (p *Provisioner) Queue(ctx context.Context, websiteID string) error {
	if websiteID == "" {
		return errors.New("website ID is required")
	}
	select {
	case p.queue <- websiteID:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("queue website: %w", ctx.Err())
	}
}

// Recover re-queues provisioning that was interrupted by a panel restart.
// Active terminal states are untouched.
func (p *Provisioner) Recover(ctx context.Context) error {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id FROM websites WHERE status IN ('pending', 'installing', 'configuring', 'validating') ORDER BY created_at`)
	if err != nil {
		return fmt.Errorf("find interrupted website provisioning: %w", err)
	}
	var websiteIDs []string
	for rows.Next() {
		var websiteID string
		if err := rows.Scan(&websiteID); err != nil {
			rows.Close()
			return fmt.Errorf("scan interrupted website provisioning: %w", err)
		}
		websiteIDs = append(websiteIDs, websiteID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read interrupted website provisioning: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close interrupted website provisioning: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, websiteID := range websiteIDs {
		if _, err := p.db.ExecContext(ctx,
			`UPDATE websites SET status = 'pending', error_message = NULL, provision_stage = 'queued', updated_at = ? WHERE id = ?`,
			now, websiteID); err != nil {
			return fmt.Errorf("reset interrupted website %s: %w", websiteID, err)
		}
		if err := p.Queue(ctx, websiteID); err != nil {
			p.fail(ctx, websiteID, "restore provisioning queue failed: "+err.Error())
			return err
		}
	}
	return nil
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
	automaticFramework := (w.SetupMode == SetupAutomatic || w.SetupMode == "automatic") && w.Framework != "none"

	// Step 1: account and runtime preparation.
	if err := p.updateStatus(ctx, websiteID, "installing", ""); err != nil {
		return
	}
	if err := p.updateProgress(ctx, websiteID, "checking dependencies", ""); err != nil {
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

	automaticSetup := w.SetupMode == SetupAutomatic || w.SetupMode == "automatic"
	if automaticSetup && w.NodeVersion != "" {
		if err := p.updateProgress(ctx, websiteID, "installing Node.js", ""); err != nil {
			return
		}
		if err := p.runtime.Install(ctx, w.WebUser, w.NodeVersion, func(output string) {
			_ = p.updateProgress(ctx, websiteID, "installing Node.js", output+"\n")
		}); err != nil {
			p.fail(ctx, websiteID, "Node.js runtime installation failed: "+err.Error())
			return
		}
	}

	// Create only account-owned support directories before an automatic install.
	// Framework document roots are promoted from staging and must not pre-exist.
	dirs := []string{logDir, homeDir + "/tmp"}
	if !automaticFramework {
		if filepath.Clean(w.DocumentRoot) == filepath.Join(homeDir, "app", "public") {
			dirs = append(dirs, filepath.Join(homeDir, "app"))
		}
		dirs = append(dirs, w.DocumentRoot)
	}
	for _, dir := range dirs {
		result, err = p.exec.RunSudo(ctx, "install", "-d", "-o", w.WebUser, "-g", w.WebUser, "-m", "0750", dir)
		if err != nil {
			p.fail(ctx, websiteID, "create directory failed: "+err.Error())
			return
		}
		if result.ExitCode != 0 {
			p.fail(ctx, websiteID, "create directory failed: "+strings.TrimSpace(result.Stderr))
			return
		}
	}

	if automaticFramework {
		if err := p.installer.Install(ctx, w, func(stage, output string) error {
			return p.updateProgress(ctx, websiteID, stage, output)
		}); err != nil {
			p.fail(ctx, websiteID, "framework installation failed: "+err.Error())
			return
		}
	}
	if err := p.ensureServingPermissions(ctx, w); err != nil {
		p.fail(ctx, websiteID, "set website permissions failed: "+err.Error())
		return
	}

	if !automaticFramework {
		defaultIndex, err := landing.WebsiteUnderDevelopment(w.Domain)
		if err != nil {
			p.fail(ctx, websiteID, "render default website page failed: "+err.Error())
			return
		}
		defaultFiles := map[string]string{"index.html": defaultIndex, "robots.txt": landing.RobotsTXT, "jenderal-landing.css": landing.CSS()}
		if NginxProfileFor(w.Framework, w.FrameworkVersion, w.AppType) == "laravel" {
			defaultFiles["index.php"] = defaultIndex
		}
		for name, content := range defaultFiles {
			if err := p.ensureWebsiteFile(ctx, w, name, content); err != nil {
				p.fail(ctx, websiteID, "create default website file failed: "+err.Error())
				return
			}
		}
	}
	if automaticFramework {
		if err := p.ensureFrameworkWritablePaths(ctx, w); err != nil {
			p.fail(ctx, websiteID, "set framework permissions failed: "+err.Error())
			return
		}
	}

	// Step 2: configuring
	if err := p.updateStatus(ctx, websiteID, "configuring", ""); err != nil {
		return
	}
	if err := p.updateProgress(ctx, websiteID, "writing configuration", ""); err != nil {
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
	profile := resolveNginxProfile(w.NginxProfile, w.Framework, w.FrameworkVersion, w.AppType)
	if profile == "laravel-octane" && w.OctanePort == 0 {
		p.fail(ctx, websiteID, "website uses the laravel-octane profile but no Octane port was allocated")
		return
	}
	vhostData := VhostData{
		Domain:            w.Domain,
		Aliases:           strings.Join(aliases, " "),
		DocumentRoot:      w.DocumentRoot,
		ACMEChallengeRoot: DefaultACMEChallengeRoot,
		LogDir:            logDir,
		PHPVersion:        w.PHPVersion,
		AppType:           w.AppType,
		Profile:           profile,
		IPv6:              p.ipv6Available(),
		SecurityInclude:   "/etc/nginx/jenderal/security/sites/" + w.ID + ".conf",
		OctanePort:        w.OctanePort,
		AppPort:           w.AppPort,
	}
	if result, err := p.exec.RunSudo(ctx, "/usr/bin/install", "-d", "-m", "0755", "/etc/nginx/jenderal/security/sites"); err != nil || result.ExitCode != 0 {
		p.fail(ctx, websiteID, "create security snippet directory failed")
		return
	}
	securityFile, checkErr := p.exec.RunSudo(ctx, "/usr/bin/test", "-f", vhostData.SecurityInclude)
	if checkErr != nil {
		p.fail(ctx, websiteID, "check security snippet failed: "+checkErr.Error())
		return
	}
	if securityFile.ExitCode != 0 {
		if err := p.writeSystemFile(ctx, "# Jenderal Traffic Guard: observe configuration not enabled yet\n", vhostData.SecurityInclude); err != nil {
			p.fail(ctx, websiteID, "write security snippet failed: "+err.Error())
			return
		}
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

	// Octane sites: install the Caddyfile and systemd unit before nginx
	// starts proxying to the loopback port. The unit only starts after the
	// automatic install finished, so config-only sites enable Octane from
	// the detail page once their code is in place.
	octaneReady := false
	if profile == "laravel-octane" && automaticFramework {
		if err := p.setupOctane(ctx, w); err != nil {
			p.fail(ctx, websiteID, "configure Octane failed: "+err.Error())
			return
		}
		octaneReady = true
	}

	// Step 3: validating
	if err := p.updateStatus(ctx, websiteID, "validating", ""); err != nil {
		return
	}
	if err := p.updateProgress(ctx, websiteID, "validating nginx", ""); err != nil {
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

	// Start the Octane server last: the project (including the published
	// frankenphp worker) is complete by now.
	if octaneReady {
		if err := p.runSystemctlOK(ctx, "start", octaneUnitName(w.ID)); err != nil {
			p.fail(ctx, websiteID, "start Octane failed: "+err.Error())
			return
		}
	}

	// Step 4: active
	_ = p.updateStatus(ctx, websiteID, "active", "")
	_ = p.updateProgress(ctx, websiteID, "active", "")
	p.logAudit(ctx, "website_provisioned", websiteID, "provisioned website "+w.Domain)
}

// setupOctane writes the site's Caddyfile and systemd unit and enables the
// unit without starting it.
func (p *Provisioner) setupOctane(ctx context.Context, w websiteRow) error {
	site := model.Website{
		ID:            w.ID,
		Domain:        w.Domain,
		WebUser:       w.WebUser,
		DocumentRoot:  w.DocumentRoot,
		PHPVersion:    w.PHPVersion,
		OctanePort:    w.OctanePort,
		OctaneWorkers: w.OctaneWorkers,
	}
	if site.OctaneWorkers <= 0 {
		site.OctaneWorkers = 4
	}
	if err := writeOctaneAssets(ctx, p.exec, site); err != nil {
		return err
	}
	p.logAudit(ctx, "octane_provisioned", w.ID, "configured Laravel Octane for "+w.Domain)
	return nil
}

// runSystemctlOK runs systemctl with the given action on a unit.
func (p *Provisioner) runSystemctlOK(ctx context.Context, action, unit string) error {
	result, err := p.exec.RunSudo(ctx, "systemctl", action, unit)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("systemctl %s %s: %s", action, unit, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// ensureServingPermissions makes the managed public directory reachable by
// the Nginx worker without exposing directory listings from the account home.
func (p *Provisioner) ensureServingPermissions(ctx context.Context, w websiteRow) error {
	if !webUserRegex.MatchString(w.WebUser) || w.WebUser != DomainToUser(w.Domain) {
		return fmt.Errorf("unsafe stored web user %q", w.WebUser)
	}
	homeDir := filepath.Join("/home", w.WebUser)
	publicRoot := filepath.Join(homeDir, "public")
	appRoot := filepath.Join(homeDir, "app")
	appPublicRoot := filepath.Join(appRoot, "public")
	documentRoot := filepath.Clean(w.DocumentRoot)
	var boundaries []string
	var permissions []struct{ mode, path string }
	switch documentRoot {
	case publicRoot:
		boundaries = []string{homeDir, publicRoot}
		permissions = []struct{ mode, path string }{{"0710", homeDir}, {"0750", publicRoot}}
	case appPublicRoot:
		boundaries = []string{homeDir, appRoot, appPublicRoot}
		permissions = []struct{ mode, path string }{{"0710", homeDir}, {"0710", appRoot}, {"0750", appPublicRoot}}
	default:
		return nil
	}
	result, err := p.exec.RunSudo(ctx, "chown", append([]string{"-h", w.WebUser + ":www-data", "--"}, boundaries...)...)
	if err != nil {
		return fmt.Errorf("assign Nginx group to website directories: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("assign Nginx group to website directories: %s", strings.TrimSpace(result.Stderr))
	}
	for _, permission := range permissions {
		result, err := p.exec.RunSudo(ctx, "-u", w.WebUser, "--", "chmod", permission.mode, "--", permission.path)
		if err != nil {
			return fmt.Errorf("chmod %s: %w", permission.path, err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("chmod %s: %s", permission.path, strings.TrimSpace(result.Stderr))
		}
	}
	return nil
}

func (p *Provisioner) ensureFrameworkWritablePaths(ctx context.Context, w websiteRow) error {
	homeDir := filepath.Join("/home", w.WebUser)
	var paths []string
	switch NginxProfileFor(w.Framework, w.FrameworkVersion, w.AppType) {
	case "laravel":
		paths = []string{filepath.Join(homeDir, "app", "storage"), filepath.Join(homeDir, "app", "bootstrap", "cache")}
	case "codeigniter4":
		paths = []string{filepath.Join(homeDir, "app", "writable")}
	default:
		return nil
	}
	for _, path := range paths {
		result, err := p.exec.RunSudo(ctx, "-u", w.WebUser, "--", "chmod", "-R", "u+rwX", "--", path)
		if err != nil {
			return err
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("chmod %s: %s", path, strings.TrimSpace(result.Stderr))
		}
	}
	return nil
}

// RepairServingPermissions reconciles managed document roots created by older
// panel versions. Custom document roots are intentionally left untouched.
func (p *Provisioner) RepairServingPermissions(ctx context.Context) error {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id, domain, document_root, web_user FROM websites WHERE status IN ('active', 'suspended') ORDER BY created_at ASC`)
	if err != nil {
		return fmt.Errorf("list websites for permission repair: %w", err)
	}
	var websites []websiteRow
	for rows.Next() {
		var website websiteRow
		if err := rows.Scan(&website.ID, &website.Domain, &website.DocumentRoot, &website.WebUser); err != nil {
			rows.Close()
			return fmt.Errorf("scan website for permission repair: %w", err)
		}
		websites = append(websites, website)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list websites for permission repair: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close website permission rows: %w", err)
	}

	var repairErrors []error
	for _, website := range websites {
		if err := p.ensureServingPermissions(ctx, website); err != nil {
			repairErrors = append(repairErrors, fmt.Errorf("website %s: %w", website.ID, err))
		}
	}
	return errors.Join(repairErrors...)
}

// ensureWebsiteFile creates a document-root file as the website user, while
// preserving any file the user has already created.
func (p *Provisioner) ensureWebsiteFile(ctx context.Context, w websiteRow, name, content string) error {
	targetPath := filepath.Join(w.DocumentRoot, name)
	exists, err := p.websitePathExists(ctx, w.WebUser, targetPath)
	if err != nil {
		return fmt.Errorf("check %s: %w", targetPath, err)
	}
	if exists {
		return nil
	}

	temporaryPath := filepath.Join(w.DocumentRoot, ".jenderal-"+name+"-"+ulid.Make().String()+".tmp")
	result, err := p.exec.RunSudoWithInput(ctx, content, "-u", w.WebUser, "--", "tee", "--", temporaryPath)
	if err != nil {
		return fmt.Errorf("stage %s: %w", targetPath, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("stage %s: %s", targetPath, strings.TrimSpace(result.Stderr))
	}
	defer func() {
		_, _ = p.exec.RunSudo(context.WithoutCancel(ctx), "-u", w.WebUser, "--", "rm", "-f", "--", temporaryPath)
	}()

	result, err = p.exec.RunSudo(ctx, "-u", w.WebUser, "--", "chmod", "0644", "--", temporaryPath)
	if err != nil {
		return fmt.Errorf("set permissions on staged %s: %w", targetPath, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("set permissions on staged %s: %s", targetPath, strings.TrimSpace(result.Stderr))
	}

	result, err = p.exec.RunSudo(ctx, "-u", w.WebUser, "--", "ln", "-T", "--", temporaryPath, targetPath)
	if err != nil {
		return fmt.Errorf("publish %s: %w", targetPath, err)
	}
	if result.ExitCode == 0 {
		return nil
	}

	// A destination created between the initial check and the hard link wins.
	// This also treats dangling symlinks as existing without following them.
	exists, checkErr := p.websitePathExists(ctx, w.WebUser, targetPath)
	if checkErr != nil {
		return fmt.Errorf("publish %s: %s (recheck: %v)", targetPath, strings.TrimSpace(result.Stderr), checkErr)
	}
	if !exists {
		return fmt.Errorf("publish %s: %s", targetPath, strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (p *Provisioner) websitePathExists(ctx context.Context, webUser, targetPath string) (bool, error) {
	for _, flag := range []string{"-e", "-L"} {
		result, err := p.exec.RunSudo(ctx, "-u", webUser, "--", "test", flag, targetPath)
		if err != nil {
			return false, err
		}
		if result.ExitCode == 0 {
			return true, nil
		}
	}
	return false, nil
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

// rollbackConfigs removes the nginx, fpm, and octane configs that were
// written during provisioning.
func (p *Provisioner) rollbackConfigs(ctx context.Context, w websiteRow) {
	confPath := "/etc/nginx/sites-available/" + w.Domain
	enabledPath := "/etc/nginx/sites-enabled/" + w.Domain
	_, _ = p.exec.RunSudo(ctx, "rm", "-f", confPath)
	_, _ = p.exec.RunSudo(ctx, "rm", "-f", enabledPath)

	if w.AppType != "static" && w.PHPVersion != "" {
		poolPath := "/etc/php/" + w.PHPVersion + "/fpm/pool.d/" + w.Domain + ".conf"
		_, _ = p.exec.RunSudo(ctx, "rm", "-f", poolPath)
	}

	if resolveNginxProfile(w.NginxProfile, w.Framework, w.FrameworkVersion, w.AppType) == "laravel-octane" {
		_ = removeOctaneAssets(ctx, p.exec, w.ID)
		_, _ = p.exec.RunSudo(ctx, "rm", "-rf", "/etc/jenderal/octane/"+w.ID)
	}
}

// fail sets the website status to failed with the given error message.
func (p *Provisioner) fail(ctx context.Context, websiteID, errMsg string) {
	terminalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	errMsg = limitProvisionError(errMsg)
	_ = p.updateStatus(terminalCtx, websiteID, "failed", errMsg)
	p.logAudit(terminalCtx, "provision_failed", websiteID, errMsg)
}

const maxProvisionErrorRunes = 4096

func limitProvisionError(message string) string {
	runes := []rune(strings.TrimSpace(message))
	if len(runes) <= maxProvisionErrorRunes {
		return string(runes)
	}
	return string(runes[:maxProvisionErrorRunes]) + "…"
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

const maxProvisionLogBytes = 256 * 1024

func (p *Provisioner) updateProgress(ctx context.Context, websiteID, stage, output string) error {
	var current string
	if err := p.db.QueryRowContext(ctx, `SELECT provision_log FROM websites WHERE id = ?`, websiteID).Scan(&current); err != nil {
		return fmt.Errorf("read provisioning progress: %w", err)
	}
	current += output
	if len(current) > maxProvisionLogBytes {
		current = current[len(current)-maxProvisionLogBytes:]
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := p.db.ExecContext(ctx, `UPDATE websites SET provision_stage = ?, provision_log = ?, updated_at = ? WHERE id = ?`, stage, current, now, websiteID); err != nil {
		return fmt.Errorf("update provisioning progress: %w", err)
	}
	return nil
}

// websiteRow holds the fields needed during provisioning.
type websiteRow struct {
	ID               string
	Domain           string
	AppType          string
	PHPVersion       string
	NodeVersion      string
	DocumentRoot     string
	WebUser          string
	AppPort          int
	AppStartCommand  string
	AppBuildCommand  string
	Framework        string
	FrameworkVersion string
	FrontendStack    string
	InertiaAdapter   string
	ProjectVariant   string
	SetupMode        string
	ProvisionStage   string
	ProvisionLog     string
	NginxProfile     string
	OctanePort       int
	OctaneWorkers    int
}

// domainRow holds the fields of a domain for provisioning.
type domainRow struct {
	Name string
	Type string
}

// loadWebsite loads the minimal website fields needed for provisioning.
func (p *Provisioner) loadWebsite(ctx context.Context, id string) (websiteRow, error) {
	var w websiteRow
	var phpVersion, framework, frameworkVersion, frontendStack, inertiaAdapter, projectVariant, setupMode, provisionStage, provisionLog sql.NullString
	err := p.db.QueryRowContext(ctx,
		`SELECT id, domain, app_type, php_version, node_version, document_root, web_user,
		        framework, framework_version, frontend_stack, inertia_adapter, project_variant, setup_mode, provision_stage, provision_log,
		        nginx_profile, app_port, app_start_command, app_build_command, octane_port, octane_workers
		 FROM websites WHERE id = ?`, id,
	).Scan(&w.ID, &w.Domain, &w.AppType, &phpVersion, &w.NodeVersion, &w.DocumentRoot, &w.WebUser,
		&framework, &frameworkVersion, &frontendStack, &inertiaAdapter, &projectVariant, &setupMode, &provisionStage, &provisionLog,
		&w.NginxProfile, &w.AppPort, &w.AppStartCommand, &w.AppBuildCommand, &w.OctanePort, &w.OctaneWorkers)
	if err != nil {
		return w, err
	}
	w.PHPVersion = phpVersion.String
	w.Framework = valueOr(framework.String, "none")
	w.FrameworkVersion = frameworkVersion.String
	w.FrontendStack = frontendStack.String
	w.InertiaAdapter = inertiaAdapter.String
	w.ProjectVariant = valueOr(projectVariant.String, "empty")
	w.SetupMode = valueOr(setupMode.String, SetupConfigOnly)
	w.ProvisionStage = provisionStage.String
	w.ProvisionLog = provisionLog.String
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
