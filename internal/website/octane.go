package website

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// execSudoOK runs a command with sudo and converts a non-zero exit into an
// error carrying stderr.
func execSudoOK(ctx context.Context, exec executor.CommandExecutor, name string, args ...string) error {
	result, err := exec.RunSudo(ctx, name, args...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = fmt.Sprintf("exit status %d", result.ExitCode)
		}
		return fmt.Errorf("%s: %s", name, message)
	}
	return nil
}

// Octane port model: application ports live in 8100–8199 and each site's
// Caddy admin endpoint (used by octane tooling) sits 100 higher in 8200–8299
// so multiple Octane sites never collide.
const (
	OctanePortMin = 8100
	OctanePortMax = 8199
	// octaneWorkersMax bounds the worker count accepted from the API.
	octaneWorkersMax = 64
	// octaneMaxRequests mirrors Octane's default graceful restart count.
	octaneMaxRequests = 500
	// frankenphpBinary is the server-wide install location managed by the
	// frankenphp module.
	frankenphpBinary = "/usr/local/bin/frankenphp"
)

var octaneUnitNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// OctaneStatus is the API view of a website's Octane runtime.
type OctaneStatus struct {
	Enabled           bool     `json:"enabled"`
	Running           bool     `json:"running"`
	Port              int      `json:"port"`
	AdminPort         int      `json:"admin_port"`
	Workers           int      `json:"workers"`
	Unit              string   `json:"unit"`
	FrankenPHPVersion string   `json:"frankenphp_version"`
	State             string   `json:"state,omitempty"`
	SubState          string   `json:"sub_state,omitempty"`
	RecentLogs        []string `json:"recent_logs,omitempty"`
	ServedBy          string   `json:"served_by,omitempty"`
}

// octaneUnitName returns the systemd unit name for a website ID.
func octaneUnitName(websiteID string) string {
	return "jenderal-octane-" + websiteID + ".service"
}

// octaneAdminPort maps an application port to its Caddy admin port.
func octaneAdminPort(port int) int {
	return port + 100
}

// octanePaths derives the project root and Caddyfile location for a website.
// Octane sites always use the Laravel layout (document root app/public), so
// the project root is the app directory.
func octanePaths(w model.Website) (projectRoot, caddyfilePath, homeDir string, err error) {
	homeDir = "/home/" + w.WebUser
	if filepath.Clean(w.DocumentRoot) != filepath.Join(homeDir, "app", "public") {
		return "", "", "", model.NewValidationError("Octane requires the Laravel layout with document root app/public")
	}
	projectRoot = filepath.Join(homeDir, "app")
	caddyfilePath = "/etc/jenderal/octane/" + w.ID + "/Caddyfile"
	return projectRoot, caddyfilePath, homeDir, nil
}

// RenderOctaneCaddyfile renders the per-site FrankenPHP Caddyfile. It binds
// only the loopback port, boots the published Octane worker, and keeps the
// admin endpoint on a site-local port.
func RenderOctaneCaddyfile(projectRoot string, port, workers int) string {
	return fmt.Sprintf(`{
	admin localhost:%d

	frankenphp {
		worker {
			file %s/public/frankenphp-worker.php
			num %d
		}
	}
}

http://127.0.0.1:%d {
	root * %s/public
	encode zstd br gzip

	php_server {
		index frankenphp-worker.php
		try_files {path} frankenphp-worker.php
		# Required for the public/storage/ directory...
		resolve_root_symlink
	}
}
`, octaneAdminPort(port), projectRoot, workers, port, projectRoot)
}

// RenderOctaneUnit renders the systemd unit that supervises one site's
// Octane server. The unit runs as the website user; FrankenPHP is found via
// the explicit PATH entry for /usr/local/bin.
func RenderOctaneUnit(domain, webUser, projectRoot, caddyfilePath, phpVersion string, port, workers int) string {
	return fmt.Sprintf(`[Unit]
Description=Jenderal Octane %s
After=network.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ExecStart=/usr/bin/php%s artisan octane:start --server=frankenphp --host=127.0.0.1 --port=%d --admin-port=%d --workers=%d --max-requests=%d --no-interaction --caddyfile=%s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, domain, webUser, projectRoot, phpVersion, port, octaneAdminPort(port), workers, octaneMaxRequests, caddyfilePath)
}

// writeOctaneAssets installs the site's Caddyfile and systemd unit and
// registers the unit with systemd (daemon-reload + enable, no start).
func writeOctaneAssets(ctx context.Context, exec executor.CommandExecutor, w model.Website) error {
	projectRoot, caddyfilePath, _, err := octanePaths(w)
	if err != nil {
		return err
	}
	if !octaneUnitNamePattern.MatchString(w.ID) || !webUserRegex.MatchString(w.WebUser) {
		return model.NewValidationError("unsafe website identity for Octane configuration")
	}
	if w.OctanePort < OctanePortMin || w.OctanePort > OctanePortMax {
		return model.NewValidationError(fmt.Sprintf("Octane port %d is outside the managed range %d-%d", w.OctanePort, OctanePortMin, OctanePortMax))
	}
	workers := w.OctaneWorkers
	if workers <= 0 {
		workers = 4
	}

	// Caddyfile: root-owned directory, world-readable file (the worker reads
	// it as the website user).
	if _, err := exec.RunSudo(ctx, "mkdir", "-p", filepath.Dir(caddyfilePath)); err != nil {
		return fmt.Errorf("create Octane config directory: %w", err)
	}
	caddyfile := RenderOctaneCaddyfile(projectRoot, w.OctanePort, workers)
	if _, err := exec.RunSudoWithInput(ctx, caddyfile, "tee", caddyfilePath); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}
	if err := execSudoOK(ctx, exec, "chmod", "0644", caddyfilePath); err != nil {
		return fmt.Errorf("mode Caddyfile: %w", err)
	}

	// systemd unit.
	unitPath := "/etc/systemd/system/" + octaneUnitName(w.ID)
	unit := RenderOctaneUnit(w.Domain, w.WebUser, projectRoot, caddyfilePath, w.PHPVersion, w.OctanePort, workers)
	if _, err := exec.RunSudo(ctx, "bash", "-c",
		fmt.Sprintf("cat > %s << 'UNITEOF'\n%sUNITEOF", unitPath, unit)); err != nil {
		return fmt.Errorf("write Octane unit: %w", err)
	}
	if err := execSudoOK(ctx, exec, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return execSudoOK(ctx, exec, "systemctl", "enable", octaneUnitName(w.ID))
}

// removeOctaneAssets stops, disables, and deletes the site's Octane unit.
func removeOctaneAssets(ctx context.Context, exec executor.CommandExecutor, websiteID string) error {
	unitName := octaneUnitName(websiteID)
	unitPath := "/etc/systemd/system/" + unitName
	_, _ = exec.RunSudo(ctx, "systemctl", "stop", unitName)
	_, _ = exec.RunSudo(ctx, "systemctl", "disable", unitName)
	_, _ = exec.RunSudo(ctx, "rm", "-f", unitPath)
	_, _ = exec.RunSudo(ctx, "systemctl", "daemon-reload")
	return nil
}

// frankenphpInstalled reports whether the server-wide FrankenPHP binary works.
func frankenphpInstalled(ctx context.Context, exec executor.CommandExecutor) (string, bool) {
	result, err := exec.Run(ctx, frankenphpBinary, "version")
	if err != nil || result == nil || result.ExitCode != 0 {
		return "", false
	}
	fields := strings.Fields(result.Stdout)
	version := ""
	for _, field := range fields {
		if strings.HasPrefix(field, "v") && len(field) >= 6 {
			version = strings.TrimPrefix(field, "v")
			break
		}
	}
	return version, true
}

// allocateOctanePort returns the lowest free port in the Octane range that is
// neither stored on another website nor currently listening.
func (s *Service) allocateOctanePort(ctx context.Context) (int, error) {
	used := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT octane_port FROM websites WHERE octane_port > 0`)
	if err != nil {
		return 0, fmt.Errorf("list octane ports: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			return 0, fmt.Errorf("scan octane ports: %w", err)
		}
		used[port] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if result, err := s.exec.RunSudo(ctx, "ss", "-tln"); err == nil && result != nil {
		for _, line := range strings.Split(result.Stdout, "\n") {
			fields := strings.Fields(line)
			// ss -tln data rows: State Recv-Q Send-Q Local:Port Peer:Port ...
			if len(fields) < 4 || fields[0] != "LISTEN" {
				continue
			}
			local := fields[3]
			if idx := strings.LastIndex(local, ":"); idx >= 0 {
				if port, err := strconv.Atoi(local[idx+1:]); err == nil {
					used[port] = true
				}
			}
		}
	}

	for port := OctanePortMin; port <= OctanePortMax; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, model.NewDomainError("OCTANE_PORT_EXHAUSTED", "no free Octane ports are available", nil)
}

// EnableOctane converts an existing Laravel site to Octane: it writes the
// Caddyfile and systemd unit, switches the nginx vhost to the laravel-octane
// proxy profile (the PHP-FPM vhost comes back with one click on disable), and
// queues the composer/octane install that ends with starting the unit.
func (s *Service) EnableOctane(ctx context.Context, id string) (string, error) {
	unlock := s.mutations.Lock(id)
	defer unlock()

	w, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if w.Framework != "laravel" && w.AppType != "laravel" {
		return "", model.NewValidationError("Octane can only be enabled on Laravel websites")
	}
	if w.Status != "active" {
		return "", model.NewValidationError("website must be active before enabling Octane")
	}
	if w.OctaneEnabled {
		return "", model.NewValidationError("Octane is already enabled for this website")
	}
	if _, ok := frankenphpInstalled(ctx, s.exec); !ok {
		return "", model.NewDomainError("FRANKENPHP_MISSING",
			"FrankenPHP is not installed; an administrator must install it first", nil)
	}
	projectRoot, _, _, err := octanePaths(w)
	if err != nil {
		return "", err
	}
	if result, err := s.exec.RunSudo(ctx, "test", "-f", filepath.Join(projectRoot, "artisan")); err != nil {
		return "", err
	} else if result.ExitCode != 0 {
		return "", model.NewValidationError("no Laravel project found in " + projectRoot)
	}

	// Allocate and persist the port first so nginx, Caddyfile, and systemd
	// always agree on it.
	if w.OctanePort == 0 {
		port, err := s.allocateOctanePort(ctx)
		if err != nil {
			return "", err
		}
		w.OctanePort = port
	}
	if w.OctaneWorkers <= 0 {
		w.OctaneWorkers = 4
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE websites SET octane_enabled = 1, octane_port = ?, nginx_profile = 'laravel-octane', updated_at = ? WHERE id = ?`,
		w.OctanePort, now, id); err != nil {
		return "", fmt.Errorf("enable octane: %w", err)
	}
	// Reload the row so the vhost regeneration below sees the new profile.
	w, err = s.Get(ctx, id)
	if err != nil {
		return "", err
	}

	if err := writeOctaneAssets(ctx, s.exec, w); err != nil {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE websites SET octane_enabled = 0, nginx_profile = '', updated_at = ? WHERE id = ?`, now, id)
		return "", err
	}

	// The PHP-FPM vhost remains on disk until the next config regeneration;
	// switching the stored profile back restores it as instant rollback.
	if err := s.regenerateConfig(ctx, w, ""); err != nil {
		return "", fmt.Errorf("switch vhost to Octane proxy: %w", err)
	}

	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
	}
	taskID := tr.Run("Enable Laravel Octane ("+w.Domain+")", "bash", "-c",
		octaneEnableScript(w, projectRoot))
	return taskID, nil
}

// octanePrepareScript renders the idempotent root-side script that makes the
// app ready for Octane: installs laravel/octane when missing, registers its
// service provider, then runs octane:install. The explicit package:discover
// matters — composer --no-scripts skips post-autoload-dump discovery, and
// without it artisan does not know the "octane" namespace at all.
func octanePrepareScript(w model.Website, projectRoot string) string {
	php := "/usr/bin/php" + w.PHPVersion
	return fmt.Sprintf(`set -eu
export HOME='/home/%[1]s'
cd '%[2]s'
if ! sudo -u '%[1]s' /usr/local/bin/composer show laravel/octane --no-ansi --working-dir='%[2]s' >/dev/null 2>&1; then
  echo "=== Installing laravel/octane via Composer ==="
  sudo -u '%[1]s' /usr/local/bin/composer require laravel/octane --no-interaction --no-scripts --working-dir='%[2]s'
fi
echo "=== Registering the Octane service provider ==="
sudo -u '%[1]s' %[3]s '%[2]s/artisan' package:discover --ansi
echo "=== Running octane:install ==="
sudo -u '%[1]s' %[3]s '%[2]s/artisan' octane:install --server=frankenphp --no-interaction
sudo -u '%[1]s' %[3]s '%[2]s/artisan' config:clear --no-interaction || true
`, w.WebUser, projectRoot, php)
}

// octaneEnableScript prepares the app and starts the unit (used by Enable).
func octaneEnableScript(w model.Website, projectRoot string) string {
	unit := octaneUnitName(w.ID)
	return octanePrepareScript(w, projectRoot) + fmt.Sprintf(`echo "=== Starting %[1]s ==="
systemctl start '%[1]s'
`, unit)
}

// DisableOctane switches a site back to PHP-FPM serving: the Octane unit is
// removed, the vhost regains the framework profile, and the flag clears. The
// allocated port is kept for reuse.
func (s *Service) DisableOctane(ctx context.Context, id string) error {
	unlock := s.mutations.Lock(id)
	defer unlock()

	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !w.OctaneEnabled {
		return model.NewValidationError("Octane is not enabled for this website")
	}

	if err := removeOctaneAssets(ctx, s.exec, w.ID); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE websites SET octane_enabled = 0, nginx_profile = '', updated_at = ? WHERE id = ?`,
		now, id); err != nil {
		return fmt.Errorf("disable octane: %w", err)
	}

	fresh, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.regenerateConfig(ctx, fresh, ""); err != nil {
		return fmt.Errorf("restore PHP-FPM vhost: %w", err)
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "octane_disable", Module: "website", Target: id, Detail: "disabled Laravel Octane for " + w.Domain})
	return nil
}

// OctaneAction runs a lifecycle action (start/stop/restart) on a site's
// Octane unit.
func (s *Service) OctaneAction(ctx context.Context, id, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return model.NewValidationError("unknown Octane action: " + action)
	}
	unlock := s.mutations.Lock(id)
	defer unlock()

	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !w.OctaneEnabled {
		return model.NewValidationError("Octane is not enabled for this website")
	}
	if err := s.runSudoOK(ctx, "systemctl", action, octaneUnitName(w.ID)); err != nil {
		return fmt.Errorf("%s octane: %w", action, err)
	}
	if action == "start" || action == "restart" {
		// Verify the worker survived: a fast crash (port already in use,
		// missing worker script, PHP error) would otherwise look like a
		// successful start. Poll — FrankenPHP can take seconds to boot.
		if err := s.waitForOctaneActive(ctx, octaneUnitName(w.ID), 20*time.Second); err != nil {
			return fmt.Errorf("%s octane: unit did not stay active — check the Octane card for recent unit logs", action)
		}
	}
	return nil
}

// ReloadOctane gracefully reloads the Octane workers (zero-downtime deploy
// step) as a background task.
func (s *Service) ReloadOctane(ctx context.Context, id string) (string, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !w.OctaneEnabled {
		return "", model.NewValidationError("Octane is not enabled for this website")
	}
	projectRoot, _, _, err := octanePaths(w)
	if err != nil {
		return "", err
	}
	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
	}
	script := fmt.Sprintf("set -eu; cd '%s' && /usr/bin/php%s artisan octane:reload --no-interaction",
		projectRoot, w.PHPVersion)
	return tr.Run("Reload Laravel Octane ("+w.Domain+")", "sudo", "-u", w.WebUser, "bash", "-c", script), nil
}

// SetOctaneWorkers persists the worker count, rewrites the unit, and restarts
// the service when it is running so the change takes effect.
func (s *Service) SetOctaneWorkers(ctx context.Context, id string, workers int) (model.Website, error) {
	if workers < 1 || workers > octaneWorkersMax {
		return model.Website{}, model.NewValidationError(fmt.Sprintf("workers must be between 1 and %d", octaneWorkersMax))
	}
	unlock := s.mutations.Lock(id)
	defer unlock()

	w, err := s.Get(ctx, id)
	if err != nil {
		return model.Website{}, err
	}
	if !w.OctaneEnabled {
		return model.Website{}, model.NewValidationError("Octane is not enabled for this website")
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE websites SET octane_workers = ?, updated_at = ? WHERE id = ?`,
		workers, time.Now().UTC().Format(time.RFC3339), id); err != nil {
		return model.Website{}, fmt.Errorf("update octane workers: %w", err)
	}

	w.OctaneWorkers = workers
	if err := writeOctaneAssets(ctx, s.exec, w); err != nil {
		return model.Website{}, err
	}
	// Restart only when the unit was active; a stopped site stays stopped.
	if result, err := s.exec.Run(ctx, "systemctl", "is-active", "--quiet", octaneUnitName(w.ID)); err == nil && result != nil && result.ExitCode == 0 {
		if err := s.runSudoOK(ctx, "systemctl", "restart", octaneUnitName(w.ID)); err != nil {
			return model.Website{}, fmt.Errorf("apply worker count: %w", err)
		}
	}
	return s.Get(ctx, id)
}

// StartOctaneTask prepares the app (installs laravel/octane and registers
// its provider when missing, runs octane:install) and starts the unit inside
// a background task, verifying it stays up. Re-running it heals a site whose
// earlier Octane preparation failed halfway.
func (s *Service) StartOctaneTask(ctx context.Context, id string) (string, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !w.OctaneEnabled {
		return "", model.NewValidationError("Octane is not enabled for this website")
	}
	projectRoot, _, _, err := octanePaths(w)
	if err != nil {
		return "", err
	}
	tr := s.taskRunner()
	if tr == nil {
		return "", fmt.Errorf("task runner not available")
	}
	unit := octaneUnitName(w.ID)
	return tr.RunFuncWithOptions(
		taskrunner.Options{Name: "Start Octane — " + w.Domain, Module: "website", Timeout: 15 * time.Minute},
		func(taskCtx context.Context, write func(string)) error {
			write("Preparing the application for Octane…")
			result, err := s.exec.RunSudo(taskCtx, "bash", "-c", octanePrepareScript(w, projectRoot))
			if result != nil && result.Stdout != "" {
				write(strings.TrimSpace(result.Stdout))
			}
			if err != nil {
				return fmt.Errorf("prepare octane: %w", err)
			}
			if result.ExitCode != 0 {
				return fmt.Errorf("prepare octane failed with exit status %d", result.ExitCode)
			}

			write("Starting " + unit + "…")
			if err := execSudoOK(taskCtx, s.exec, "systemctl", "start", unit); err != nil {
				return fmt.Errorf("start octane: %w", err)
			}
			if err := s.waitForOctaneActive(taskCtx, unit, 20*time.Second); err != nil {
				if logs := s.octaneUnitLogs(taskCtx, unit, 15); len(logs) > 0 {
					write(strings.Join(logs, "\n"))
				}
				return fmt.Errorf("octane unit did not stay active — see the logs above")
			}
			write("Octane is running.")
			return nil
		},
	), nil
}

// OctaneStatus reports the runtime state of a site's Octane server.
func (s *Service) OctaneStatus(ctx context.Context, id string) (OctaneStatus, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return OctaneStatus{}, err
	}
	status := OctaneStatus{
		Enabled: w.OctaneEnabled,
		Port:    w.OctanePort,
		Workers: w.OctaneWorkers,
	}
	if w.OctaneEnabled {
		status.AdminPort = octaneAdminPort(w.OctanePort)
		status.Unit = octaneUnitName(w.ID)
		result, err := s.exec.Run(ctx, "systemctl", "is-active", "--quiet", status.Unit)
		status.Running = err == nil && result != nil && result.ExitCode == 0
		status.State, status.SubState = s.octaneUnitState(ctx, status.Unit)
		// Journal context is failure diagnostics: a deliberate Stop settles
		// in inactive/dead and needs no "why did it stop" noise, while a
		// crashed start/restart ends in failed.
		if !status.Running && (status.State == "failed" || status.State == "") {
			status.RecentLogs = s.octaneUnitLogs(ctx, status.Unit, 15)
		}
		if status.Running {
			// Through Nginx the browser only ever sees Nginx's own Server
			// header; hitting the Octane port directly reveals the actual
			// upstream (FrankenPHP answers as Caddy) — visible proof the
			// site is being served by Octane.
			status.ServedBy = probeOctaneServedBy(ctx, w.OctanePort, w.Domain)
		}
	}
	if version, ok := frankenphpInstalled(ctx, s.exec); ok {
		status.FrankenPHPVersion = version
	}
	return status, nil
}

// waitForOctaneActive polls until the unit reports active, the budget runs
// out, or the unit settles in a failed state. FrankenPHP workers can take
// several seconds to boot, so a single immediate check gives false failures.
func (s *Service) waitForOctaneActive(ctx context.Context, unit string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		result, err := s.exec.Run(ctx, "systemctl", "is-active", "--quiet", unit)
		if err == nil && result != nil && result.ExitCode == 0 {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if state, _ := s.octaneUnitState(ctx, unit); state == "failed" {
			return fmt.Errorf("unit entered failed state")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("unit did not become active within %s", budget)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// octaneUnitState queries systemd for the unit's fine-grained state, e.g.
// "active running", "failed auto-restart", "inactive dead". Best effort:
// returns empty strings when systemd cannot be reached.
func (s *Service) octaneUnitState(ctx context.Context, unit string) (string, string) {
	result, err := s.exec.Run(ctx, "systemctl", "show", unit,
		"--property=ActiveState", "--property=SubState", "--value")
	if err != nil || result == nil || result.ExitCode != 0 {
		return "", ""
	}
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	state, sub := "", ""
	if len(lines) > 0 {
		state = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		sub = strings.TrimSpace(lines[1])
	}
	return state, sub
}

// octaneUnitLogs returns the unit's last journal lines (root-side, since
// system unit logs are not readable by unprivileged users).
func (s *Service) octaneUnitLogs(ctx context.Context, unit string, n int) []string {
	result, err := s.exec.RunSudo(ctx, "journalctl", "-u", unit, "-n", fmt.Sprintf("%d", n), "--no-pager", "-o", "cat")
	if err != nil || result == nil || result.ExitCode != 0 || strings.TrimSpace(result.Stdout) == "" {
		return nil
	}
	logs := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	if len(logs) > n {
		logs = logs[len(logs)-n:]
	}
	return logs
}

// probeOctaneServedBy sends a HEAD request straight to the Octane port and
// returns the upstream's Server header (FrankenPHP is Caddy-based, so it
// answers "Caddy"). Empty when the probe is inconclusive.
func probeOctaneServedBy(ctx context.Context, port int, domain string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	if err != nil {
		return ""
	}
	req.Host = domain
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	return resp.Header.Get("Server")
}
