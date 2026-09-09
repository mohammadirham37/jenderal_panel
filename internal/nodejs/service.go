package nodejs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/noderuntime"
	"github.com/mohammadirham37/jenderal_panel/internal/siteops"
)

type VersionInfo struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	LTS       bool   `json:"lts"`
}

var supportedNodeVersions = []VersionInfo{
	{Version: "20", LTS: true},
	{Version: "22", LTS: true},
	{Version: "24", LTS: true},
}

// CreateAppRequest holds the data for creating a new Node.js app.
type CreateAppRequest struct {
	WebsiteID   string `json:"website_id"`
	NodeVersion string `json:"node_version"`
	PackageMgr  string `json:"package_mgr"`
	BuildCmd    string `json:"build_cmd"`
	StartCmd    string `json:"start_cmd"`
	Port        int    `json:"port"`
	EnvVars     string `json:"env_vars"`
}

// Service manages Node.js application lifecycle.
type Service struct {
	db          *sql.DB
	exec        executor.CommandExecutor
	audit       *audit.Service
	runtime     runtimeManager
	mutations   *siteops.Coordinator
	runtimeGate sync.RWMutex
}

// NewService creates a new Node.js management service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc, runtime: noderuntime.New(exec), mutations: siteops.Default}
}

// ListVersions returns the supported catalog. Installation is owned per website,
// so this compatibility endpoint never probes a global executable.
func (s *Service) ListVersions(ctx context.Context) ([]VersionInfo, error) {
	return append([]VersionInfo(nil), supportedNodeVersions...), nil
}

func (s *Service) validateVersion(version string) error {
	if err := noderuntime.ValidateVersion(version); err == nil {
		return nil
	}
	return model.NewValidationError("unsupported Node.js version: " + version)
}

// Install is retained only to give older clients an actionable migration error.
func (s *Service) Install(ctx context.Context, version string) error {
	return model.NewValidationError("global Node.js installation is no longer managed; select a website runtime and use /api/v1/nodejs/runtimes/{websiteID}")
}

// CreateApp creates a new Node.js application, writes systemd and nginx configs,
// and reloads the relevant services.
func (s *Service) CreateApp(ctx context.Context, req CreateAppRequest) (model.NodeApp, error) {
	if req.WebsiteID == "" {
		return model.NodeApp{}, model.NewValidationError("website_id is required")
	}
	req.StartCmd = strings.TrimSpace(req.StartCmd)
	if req.StartCmd == "" {
		return model.NodeApp{}, model.NewValidationError("start_cmd is required")
	}
	if containsLineControl(req.StartCmd) {
		return model.NodeApp{}, model.NewValidationError("start_cmd must not contain newlines or NUL")
	}
	if req.Port <= 0 {
		return model.NodeApp{}, model.NewValidationError("port must be a positive integer")
	}

	if req.PackageMgr == "" {
		req.PackageMgr = "npm"
	}
	validPkgMgrs := map[string]bool{"npm": true, "yarn": true, "pnpm": true}
	if !validPkgMgrs[req.PackageMgr] {
		return model.NodeApp{}, model.NewValidationError("package_mgr must be npm, yarn, or pnpm")
	}

	if _, err := parseEnvironment(req.EnvVars); err != nil {
		return model.NodeApp{}, err
	}

	s.runtimeGate.RLock()
	defer s.runtimeGate.RUnlock()
	unlock := s.mutations.Lock(req.WebsiteID)
	defer unlock()
	website, err := s.loadWebsiteRuntime(ctx, req.WebsiteID)
	if err != nil {
		return model.NodeApp{}, err
	}
	if website.NodeVersion == "" {
		return model.NodeApp{}, model.NewValidationError("this website has no Node.js runtime selected; choose and install one from /nodejs")
	}
	if containsLineControl(website.Domain) {
		return model.NodeApp{}, fmt.Errorf("unsafe website domain %q", website.Domain)
	}
	if req.NodeVersion != "" && req.NodeVersion != website.NodeVersion {
		return model.NodeApp{}, model.NewValidationError("node_version is inherited from the website and must be " + website.NodeVersion)
	}
	status, err := s.runtime.Detect(ctx, website.WebUser, website.NodeVersion)
	if err != nil {
		return model.NodeApp{}, fmt.Errorf("detect website Node.js runtime: %w", err)
	}
	if !status.Installed {
		return model.NodeApp{}, model.NewValidationError("Node.js " + website.NodeVersion + " is not installed for this website; install or retry it from /nodejs")
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	id := ulid.Make().String()

	app := model.NodeApp{
		ID:          id,
		WebsiteID:   req.WebsiteID,
		NodeVersion: website.NodeVersion,
		PackageMgr:  req.PackageMgr,
		BuildCmd:    req.BuildCmd,
		StartCmd:    req.StartCmd,
		Port:        req.Port,
		EnvVars:     req.EnvVars,
		Status:      "stopped",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	unitContent, err := s.buildSystemdUnit(app, website.WebUser, website.DocumentRoot)
	if err != nil {
		return model.NodeApp{}, err
	}
	unitPath := "/etc/systemd/system/jenderal-node-" + id + ".service"
	if err := s.writeFileViaSudo(ctx, unitPath, unitContent); err != nil {
		return model.NodeApp{}, fmt.Errorf("write systemd unit: %w", err)
	}

	proxyContent := s.buildNginxProxy(app.Port, website.Domain)
	proxyPath := "/etc/nginx/conf.d/jenderal-node-" + id + ".conf"
	if err := s.writeFileViaSudo(ctx, proxyPath, proxyContent); err != nil {
		primary := fmt.Errorf("write nginx proxy config: %w", err)
		return model.NodeApp{}, joinCleanupError(primary, s.cleanupCreatedConfigs(ctx, unitPath, proxyPath))
	}

	if err := s.runSudoOK(ctx, "systemctl", "daemon-reload"); err != nil {
		primary := fmt.Errorf("reload systemd: %w", err)
		return model.NodeApp{}, joinCleanupError(primary, s.cleanupCreatedConfigs(ctx, unitPath, proxyPath))
	}
	if err := s.runSudoOK(ctx, "systemctl", "reload", "nginx"); err != nil {
		primary := fmt.Errorf("reload nginx: %w", err)
		return model.NodeApp{}, joinCleanupError(primary, s.cleanupCreatedConfigs(ctx, unitPath, proxyPath))
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO nodejs_apps (id, website_id, node_version, package_mgr, build_cmd, start_cmd, port, env_vars, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		app.ID, app.WebsiteID, app.NodeVersion, app.PackageMgr,
		nullableString(app.BuildCmd), app.StartCmd, app.Port,
		nullableString(app.EnvVars), app.Status,
		nowStr, nowStr,
	)
	if err != nil {
		primary := fmt.Errorf("insert nodejs app: %w", err)
		return model.NodeApp{}, joinCleanupError(primary, s.cleanupCreatedConfigs(ctx, unitPath, proxyPath))
	}

	return app, nil
}

func (s *Service) cleanupCreatedConfigs(ctx context.Context, paths ...string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var failures []error
	for _, path := range paths {
		if err := s.runSudoOK(cleanupCtx, "rm", "-f", "--", path); err != nil {
			failures = append(failures, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	if err := s.runSudoOK(cleanupCtx, "systemctl", "daemon-reload"); err != nil {
		failures = append(failures, fmt.Errorf("reload systemd after cleanup: %w", err))
	}
	if err := s.runSudoOK(cleanupCtx, "systemctl", "reload", "nginx"); err != nil {
		failures = append(failures, fmt.Errorf("reload nginx after cleanup: %w", err))
	}
	return errors.Join(failures...)
}

func joinCleanupError(primary, cleanup error) error {
	if cleanup == nil {
		return primary
	}
	return errors.Join(primary, fmt.Errorf("cleanup failed: %w", cleanup))
}

// Get returns a Node.js app by ID.
func (s *Service) Get(ctx context.Context, id string) (model.NodeApp, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, website_id, node_version, package_mgr, build_cmd, start_cmd,
		        port, env_vars, status, created_at, updated_at
		 FROM nodejs_apps WHERE id = ?`, id)

	app, err := scanNodeApp(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.NodeApp{}, model.ErrNotFound
		}
		return model.NodeApp{}, fmt.Errorf("get nodejs app: %w", err)
	}
	return app, nil
}

// List returns all Node.js apps ordered by created_at DESC.
func (s *Service) List(ctx context.Context) ([]model.NodeApp, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, node_version, package_mgr, build_cmd, start_cmd,
		        port, env_vars, status, created_at, updated_at
		 FROM nodejs_apps ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list nodejs apps: %w", err)
	}
	defer rows.Close()

	var apps []model.NodeApp
	for rows.Next() {
		app, err := scanNodeAppRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan nodejs app: %w", err)
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

// ListByWebsite returns all Node.js apps for a specific website.
func (s *Service) ListByWebsite(ctx context.Context, websiteID string) ([]model.NodeApp, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, node_version, package_mgr, build_cmd, start_cmd,
		        port, env_vars, status, created_at, updated_at
		 FROM nodejs_apps WHERE website_id = ? ORDER BY created_at DESC`, websiteID)
	if err != nil {
		return nil, fmt.Errorf("list nodejs apps by website: %w", err)
	}
	defer rows.Close()

	var apps []model.NodeApp
	for rows.Next() {
		app, err := scanNodeAppRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan nodejs app: %w", err)
		}
		apps = append(apps, app)
	}
	return apps, rows.Err()
}

// Start starts a Node.js app via systemctl and updates the DB status.
func (s *Service) Start(ctx context.Context, id string) error {
	app, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := validateNodeAppID(app.ID); err != nil {
		return err
	}
	unlock := s.mutations.Lock(app.WebsiteID)
	defer unlock()
	if _, _, err := s.ensureAppRuntime(ctx, id); err != nil {
		return err
	}

	svcName := "jenderal-node-" + id + ".service"
	result, err := s.exec.RunSudo(ctx, "systemctl", "start", svcName)
	if err != nil {
		return fmt.Errorf("start %s: %w", svcName, err)
	}
	if result.ExitCode != 0 {
		_ = s.updateStatus(ctx, id, "failed")
		return fmt.Errorf("start %s: %s", svcName, strings.TrimSpace(result.Stderr))
	}

	_, _ = s.exec.RunSudo(ctx, "systemctl", "enable", svcName)

	return s.updateStatus(ctx, id, "running")
}

// Stop stops a Node.js app via systemctl and updates the DB status.
func (s *Service) Stop(ctx context.Context, id string) error {
	app, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := validateNodeAppID(app.ID); err != nil {
		return err
	}
	unlock := s.mutations.Lock(app.WebsiteID)
	defer unlock()
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}

	svcName := "jenderal-node-" + id + ".service"
	result, err := s.exec.RunSudo(ctx, "systemctl", "stop", svcName)
	if err != nil {
		return fmt.Errorf("stop %s: %w", svcName, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("stop %s: %s", svcName, strings.TrimSpace(result.Stderr))
	}

	return s.updateStatus(ctx, id, "stopped")
}

// Restart restarts a Node.js app via systemctl and updates the DB status.
func (s *Service) Restart(ctx context.Context, id string) error {
	app, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := validateNodeAppID(app.ID); err != nil {
		return err
	}
	unlock := s.mutations.Lock(app.WebsiteID)
	defer unlock()
	if _, _, err := s.ensureAppRuntime(ctx, id); err != nil {
		return err
	}

	svcName := "jenderal-node-" + id + ".service"
	result, err := s.exec.RunSudo(ctx, "systemctl", "restart", svcName)
	if err != nil {
		return fmt.Errorf("restart %s: %w", svcName, err)
	}
	if result.ExitCode != 0 {
		_ = s.updateStatus(ctx, id, "failed")
		return fmt.Errorf("restart %s: %s", svcName, strings.TrimSpace(result.Stderr))
	}

	return s.updateStatus(ctx, id, "running")
}

func (s *Service) ensureAppRuntime(ctx context.Context, id string) (model.NodeApp, websiteRuntimeRow, error) {
	app, err := s.Get(ctx, id)
	if err != nil {
		return model.NodeApp{}, websiteRuntimeRow{}, err
	}
	website, err := s.loadWebsiteRuntime(ctx, app.WebsiteID)
	if err != nil {
		return model.NodeApp{}, websiteRuntimeRow{}, err
	}
	if website.NodeVersion == "" {
		return model.NodeApp{}, websiteRuntimeRow{}, model.NewValidationError("this website has no Node.js runtime selected; choose and install one from /nodejs")
	}
	status, err := s.runtime.Detect(ctx, website.WebUser, website.NodeVersion)
	if err != nil {
		return model.NodeApp{}, websiteRuntimeRow{}, fmt.Errorf("detect website Node.js runtime: %w", err)
	}
	if !status.Installed {
		return model.NodeApp{}, websiteRuntimeRow{}, model.NewValidationError("Node.js " + website.NodeVersion + " is not installed for this website; install or retry it from /nodejs")
	}
	return app, website, nil
}

// Delete stops and removes a Node.js app, its systemd unit, and nginx proxy config.
func (s *Service) Delete(ctx context.Context, id string) error {
	app, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := validateNodeAppID(app.ID); err != nil {
		return err
	}
	s.runtimeGate.RLock()
	defer s.runtimeGate.RUnlock()
	unlock := s.mutations.Lock(app.WebsiteID)
	defer unlock()
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}

	svcName := "jenderal-node-" + id + ".service"

	// Stop and disable systemd service.
	_, _ = s.exec.RunSudo(ctx, "systemctl", "stop", svcName)
	_, _ = s.exec.RunSudo(ctx, "systemctl", "disable", svcName)

	// Remove systemd unit file.
	unitPath := "/etc/systemd/system/" + svcName
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", unitPath)

	// Remove nginx proxy config.
	proxyPath := "/etc/nginx/conf.d/jenderal-node-" + id + ".conf"
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", proxyPath)

	// Reload systemd and nginx.
	_, _ = s.exec.RunSudo(ctx, "systemctl", "daemon-reload")
	_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")

	// Delete DB record.
	_, err = s.db.ExecContext(ctx, `DELETE FROM nodejs_apps WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete nodejs app: %w", err)
	}

	return nil
}

// updateStatus updates the status and updated_at timestamp for a Node.js app.
func (s *Service) updateStatus(ctx context.Context, id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE nodejs_apps SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id,
	)
	if err != nil {
		return fmt.Errorf("update nodejs app status: %w", err)
	}
	return nil
}

// buildSystemdUnit generates a systemd unit file for a Node.js app.
func (s *Service) buildSystemdUnit(app model.NodeApp, webUser, docRoot string) (string, error) {
	if err := validateNodeAppID(app.ID); err != nil {
		return "", err
	}
	if err := noderuntime.ValidateVersion(app.NodeVersion); err != nil {
		return "", err
	}
	home, err := noderuntime.Home(webUser)
	if err != nil {
		return "", err
	}
	cleanRoot := filepath.Clean(docRoot)
	if cleanRoot != home && !strings.HasPrefix(cleanRoot, home+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe website document root %q", docRoot)
	}
	if containsLineControl(cleanRoot) || containsLineControl(app.StartCmd) {
		return "", model.NewValidationError("systemd command fields must not contain newlines or NUL")
	}
	env, err := parseEnvironment(app.EnvVars)
	if err != nil {
		return "", err
	}
	var b strings.Builder

	b.WriteString("[Unit]\n")
	b.WriteString(fmt.Sprintf("Description=Jenderal Node App %s\n", app.ID))
	b.WriteString("\n")

	b.WriteString("[Service]\n")
	b.WriteString(fmt.Sprintf("User=%s\n", webUser))
	b.WriteString("WorkingDirectory=" + systemdQuote(cleanRoot) + "\n")
	b.WriteString("Environment=" + systemdQuote("NVM_DIR="+home+"/.nvm") + "\n")
	b.WriteString("Environment=" + systemdQuote("NODE_VERSION="+app.NodeVersion) + "\n")
	b.WriteString("Environment=" + systemdQuote("PATH=/usr/local/bin:/usr/bin:/bin") + "\n")

	executable := "node"
	switch app.PackageMgr {
	case "yarn":
		executable = "yarn"
	case "pnpm":
		executable = "pnpm"
	}
	b.WriteString(fmt.Sprintf("ExecStart=%s/.nvm/nvm-exec %s %s\n", home, executable, escapeSystemdCommand(app.StartCmd)))

	b.WriteString("Environment=" + systemdQuote(fmt.Sprintf("PORT=%d", app.Port)) + "\n")
	b.WriteString("Environment=" + systemdQuote("NODE_ENV=production") + "\n")

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteString("Environment=" + systemdQuote(key+"="+env[key]) + "\n")
	}

	b.WriteString("Restart=always\n")
	b.WriteString("RestartSec=5\n")
	b.WriteString("\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return b.String(), nil
}

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var nodeAppIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validateNodeAppID(id string) error {
	if !nodeAppIDPattern.MatchString(id) {
		return fmt.Errorf("unsafe Node.js application ID %q", id)
	}
	return nil
}

func parseEnvironment(raw string) (map[string]string, error) {
	env := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return env, nil
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return nil, model.NewValidationError("env_vars must be a JSON object containing string values")
	}
	reserved := map[string]bool{"NVM_DIR": true, "NODE_VERSION": true, "PATH": true}
	for key, value := range env {
		if !environmentName.MatchString(key) {
			return nil, model.NewValidationError("invalid environment variable name: " + key)
		}
		if reserved[key] {
			return nil, model.NewValidationError(key + " is managed by the website runtime and cannot be overridden")
		}
		if containsLineControl(value) {
			return nil, model.NewValidationError("environment variable values must not contain newlines or NUL")
		}
	}
	return env, nil
}

func containsLineControl(value string) bool {
	return strings.ContainsAny(value, "\r\n\x00")
}

func systemdQuote(value string) string {
	value = strings.ReplaceAll(value, "%", "%%")
	return strconv.Quote(value)
}

func escapeSystemdCommand(value string) string {
	return strings.ReplaceAll(value, "%", "%%")
}

// buildNginxProxy generates an nginx reverse proxy configuration snippet.
func (s *Service) buildNginxProxy(port int, domain string) string {
	return fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    location / {
        proxy_pass http://127.0.0.1:%d;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
`, domain, port)
}

// writeFileViaSudo writes content to a temp file then copies it via sudo.
func (s *Service) writeFileViaSudo(ctx context.Context, destPath, content string) error {
	tmpPath := "/tmp/jenderal_nodejs_" + ulid.Make().String() + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	defer os.Remove(tmpPath)

	result, err := s.exec.RunSudo(ctx, "cp", tmpPath, destPath)
	if err != nil {
		return fmt.Errorf("copy to %s: %w", destPath, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("copy to %s: %s", destPath, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// scanNodeApp scans a single NodeApp row from *sql.Row.
func scanNodeApp(row *sql.Row) (model.NodeApp, error) {
	var app model.NodeApp
	var buildCmd, envVars sql.NullString
	var createdStr, updatedStr string

	err := row.Scan(
		&app.ID, &app.WebsiteID, &app.NodeVersion, &app.PackageMgr,
		&buildCmd, &app.StartCmd, &app.Port, &envVars,
		&app.Status, &createdStr, &updatedStr,
	)
	if err != nil {
		return app, err
	}

	app.BuildCmd = buildCmd.String
	app.EnvVars = envVars.String
	app.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	app.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return app, nil
}

// scanNodeAppRows scans a single NodeApp row from *sql.Rows.
func scanNodeAppRows(rows *sql.Rows) (model.NodeApp, error) {
	var app model.NodeApp
	var buildCmd, envVars sql.NullString
	var createdStr, updatedStr string

	err := rows.Scan(
		&app.ID, &app.WebsiteID, &app.NodeVersion, &app.PackageMgr,
		&buildCmd, &app.StartCmd, &app.Port, &envVars,
		&app.Status, &createdStr, &updatedStr,
	)
	if err != nil {
		return app, err
	}

	app.BuildCmd = buildCmd.String
	app.EnvVars = envVars.String
	app.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	app.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return app, nil
}

// nullableString returns nil if s is empty, otherwise returns s.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
