package nodejs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type VersionInfo struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	LTS       bool   `json:"lts"`
}

var supportedNodeVersions = []VersionInfo{
	{Version: "20", LTS: true},
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
	db    *sql.DB
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new Node.js management service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc}
}

// ListVersions returns supported Node.js versions and marks the installed major.
func (s *Service) ListVersions(ctx context.Context) ([]VersionInfo, error) {
	versions := append([]VersionInfo(nil), supportedNodeVersions...)
	result, err := s.exec.Run(ctx, "node", "--version")
	if err != nil {
		if errors.Is(err, osexec.ErrNotFound) {
			return versions, nil
		}
		return nil, fmt.Errorf("check node version: %w", err)
	}
	if result.ExitCode != 0 {
		return versions, nil
	}

	version := strings.TrimSpace(result.Stdout)
	if version == "" {
		return versions, nil
	}
	version = strings.TrimPrefix(version, "v")
	major := strings.SplitN(version, ".", 2)[0]
	for i := range versions {
		versions[i].Installed = versions[i].Version == major
	}

	return versions, nil
}

func (s *Service) validateVersion(version string) error {
	for _, supported := range supportedNodeVersions {
		if supported.Version == version {
			return nil
		}
	}
	return model.NewValidationError("unsupported Node.js version: " + version)
}

// Install installs Node.js using apt-get.
func (s *Service) Install(ctx context.Context, version string) error {
	if version == "" {
		return model.NewValidationError("version is required")
	}

	result, err := s.exec.RunSudo(ctx, "apt-get", "install", "-y", "nodejs")
	if err != nil {
		return fmt.Errorf("install nodejs: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("install nodejs: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

// CreateApp creates a new Node.js application, writes systemd and nginx configs,
// and reloads the relevant services.
func (s *Service) CreateApp(ctx context.Context, req CreateAppRequest) (model.NodeApp, error) {
	if req.WebsiteID == "" {
		return model.NodeApp{}, model.NewValidationError("website_id is required")
	}
	if req.StartCmd == "" {
		return model.NodeApp{}, model.NewValidationError("start_cmd is required")
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

	// Validate env_vars JSON if provided.
	if req.EnvVars != "" {
		var envMap map[string]string
		if err := json.Unmarshal([]byte(req.EnvVars), &envMap); err != nil {
			return model.NodeApp{}, model.NewValidationError("env_vars must be valid JSON object")
		}
	}

	// Fetch website to get web_user, document_root, domain.
	webUser, docRoot, domain, err := s.getWebsiteInfo(ctx, req.WebsiteID)
	if err != nil {
		return model.NodeApp{}, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	id := ulid.Make().String()

	app := model.NodeApp{
		ID:          id,
		WebsiteID:   req.WebsiteID,
		NodeVersion: req.NodeVersion,
		PackageMgr:  req.PackageMgr,
		BuildCmd:    req.BuildCmd,
		StartCmd:    req.StartCmd,
		Port:        req.Port,
		EnvVars:     req.EnvVars,
		Status:      "stopped",
		CreatedAt:   now,
		UpdatedAt:   now,
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
		return model.NodeApp{}, fmt.Errorf("insert nodejs app: %w", err)
	}

	// Write systemd unit file.
	unitContent := s.buildSystemdUnit(app, webUser, docRoot)
	unitPath := "/etc/systemd/system/jenderal-node-" + id + ".service"
	if err := s.writeFileViaSudo(ctx, unitPath, unitContent); err != nil {
		return model.NodeApp{}, fmt.Errorf("write systemd unit: %w", err)
	}

	// Write nginx reverse proxy snippet.
	proxyContent := s.buildNginxProxy(app.Port, domain)
	proxyPath := "/etc/nginx/conf.d/jenderal-node-" + id + ".conf"
	if err := s.writeFileViaSudo(ctx, proxyPath, proxyContent); err != nil {
		return model.NodeApp{}, fmt.Errorf("write nginx proxy config: %w", err)
	}

	// Reload systemd and nginx.
	_, _ = s.exec.RunSudo(ctx, "systemctl", "daemon-reload")
	_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")

	return app, nil
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
	if _, err := s.Get(ctx, id); err != nil {
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
	if _, err := s.Get(ctx, id); err != nil {
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

// Delete stops and removes a Node.js app, its systemd unit, and nginx proxy config.
func (s *Service) Delete(ctx context.Context, id string) error {
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
	_, err := s.db.ExecContext(ctx, `DELETE FROM nodejs_apps WHERE id = ?`, id)
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

// getWebsiteInfo fetches web_user, document_root, and domain for a website.
func (s *Service) getWebsiteInfo(ctx context.Context, websiteID string) (webUser, docRoot, domain string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT web_user, document_root, domain FROM websites WHERE id = ?`,
		websiteID,
	).Scan(&webUser, &docRoot, &domain)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", model.ErrNotFound
		}
		return "", "", "", fmt.Errorf("get website info: %w", err)
	}
	return webUser, docRoot, domain, nil
}

// buildSystemdUnit generates a systemd unit file for a Node.js app.
func (s *Service) buildSystemdUnit(app model.NodeApp, webUser, docRoot string) string {
	var b strings.Builder

	b.WriteString("[Unit]\n")
	b.WriteString(fmt.Sprintf("Description=Jenderal Node App %s\n", app.ID))
	b.WriteString("\n")

	b.WriteString("[Service]\n")
	b.WriteString(fmt.Sprintf("User=%s\n", webUser))
	b.WriteString(fmt.Sprintf("WorkingDirectory=%s\n", docRoot))

	// Build ExecStart based on package manager.
	switch app.PackageMgr {
	case "yarn":
		b.WriteString(fmt.Sprintf("ExecStart=/usr/bin/yarn %s\n", app.StartCmd))
	case "pnpm":
		b.WriteString(fmt.Sprintf("ExecStart=/usr/bin/pnpm %s\n", app.StartCmd))
	default:
		b.WriteString(fmt.Sprintf("ExecStart=/usr/bin/node %s\n", app.StartCmd))
	}

	b.WriteString(fmt.Sprintf("Environment=PORT=%d\n", app.Port))
	b.WriteString("Environment=NODE_ENV=production\n")

	// Parse and add additional env vars from JSON.
	if app.EnvVars != "" {
		var envMap map[string]string
		if err := json.Unmarshal([]byte(app.EnvVars), &envMap); err == nil {
			for k, v := range envMap {
				b.WriteString(fmt.Sprintf("Environment=%s=%s\n", k, v))
			}
		}
	}

	b.WriteString("Restart=always\n")
	b.WriteString("RestartSec=5\n")
	b.WriteString("\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return b.String()
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
