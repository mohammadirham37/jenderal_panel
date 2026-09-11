package website

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
	nginxconfig "github.com/mohammadirham37/jenderal_panel/internal/nginx"
	"github.com/mohammadirham37/jenderal_panel/internal/siteops"
)

// domainRegex validates domain names: alphanumeric, hyphens, dots.
var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?)*$`)
var webUserRegex = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)
var phpVersionRegex = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// CreateRequest holds the data for creating a new website.
type CreateRequest struct {
	Domain           string `json:"domain"`
	AppType          string `json:"app_type,omitempty"`
	PHPVersion       string `json:"php_version"`
	NodeVersion      string `json:"node_version"`
	Template         string `json:"template"`
	FrameworkVersion string `json:"framework_version"`
	FrontendStack    string `json:"frontend_stack"`
	InertiaAdapter   string `json:"inertia_adapter"`
	ProjectVariant   string `json:"project_variant"`
	SetupMode        string `json:"setup_mode"`
	// CreatedBy is stamped by the handler from the authenticated user; it is
	// never accepted from the request body.
	CreatedBy string `json:"-"`
}

// UpdateRequest holds the optional fields for updating a website.
type UpdateRequest struct {
	PHPVersion   *string `json:"php_version"`
	DocumentRoot *string `json:"document_root"`
}

// Service manages website CRUD operations and lifecycle.
type Service struct {
	db            *sql.DB
	exec          executor.CommandExecutor
	audit         *audit.Service
	prov          *Provisioner
	ipv6Available func() bool
	mutations     *siteops.Coordinator
	tasks         *taskrunner.Runner
}

// NewService creates a new website management service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc, ipv6Available: nginxconfig.IPv6Available, mutations: siteops.Default}
}

// SetProvisioner sets the provisioner after creation to break circular
// dependency between Service and Provisioner.
func (s *Service) SetProvisioner(p *Provisioner) {
	s.prov = p
}

type RuntimeOption struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
}

type DependencyOption struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	ManageURL string `json:"manage_url"`
}

type ProfileOption struct {
	Template         string                `json:"template"`
	FrameworkVersion string                `json:"framework_version"`
	FrontendStack    string                `json:"frontend_stack"`
	InertiaAdapter   string                `json:"inertia_adapter"`
	ProjectVariant   string                `json:"project_variant"`
	SetupMode        string                `json:"setup_mode"`
	Enabled          bool                  `json:"enabled"`
	Reason           string                `json:"reason"`
	MinimumPHP       string                `json:"minimum_php"`
	DocumentRoot     string                `json:"document_root"`
	Prerequisites    []string              `json:"prerequisites"`
	PHPCompatibility []CompatibilityOption `json:"php_compatibility"`
}

type CompatibilityOption struct {
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

type WebsiteOptions struct {
	PHPVersions     []RuntimeOption    `json:"php_versions"`
	NodeVersions    []string           `json:"node_versions"`
	Dependencies    []DependencyOption `json:"dependencies"`
	Profiles        []ProfileOption    `json:"profiles"`
	InertiaAdapters []string           `json:"inertia_adapters"`
	Defaults        CreateRequest      `json:"defaults"`
}

// Options reports host runtimes separately from the fixed website profile catalog.
func (s *Service) Options(ctx context.Context) (WebsiteOptions, error) {
	options := WebsiteOptions{NodeVersions: []string{"20", "22", "24"}, InertiaAdapters: []string{"react", "vue", "svelte"}, Defaults: CreateRequest{
		Template: "php", FrameworkVersion: "12", FrontendStack: "blade", ProjectVariant: "empty", SetupMode: SetupConfigOnly,
	}}
	for _, version := range []string{"8.1", "8.2", "8.3", "8.4"} {
		installed, err := s.phpRuntimeInstalled(ctx, version)
		if err != nil {
			return WebsiteOptions{}, fmt.Errorf("check PHP %s: %w", version, err)
		}
		runtime := RuntimeOption{Version: version, Installed: installed}
		if runtime.Installed {
			serviceStatus, statusErr := s.exec.Run(ctx, "systemctl", "is-active", "--quiet", "php"+version+"-fpm")
			if statusErr != nil {
				return WebsiteOptions{}, fmt.Errorf("check PHP %s FPM: %w", version, statusErr)
			}
			runtime.Running = serviceStatus.ExitCode == 0
		}
		options.PHPVersions = append(options.PHPVersions, runtime)
		if installed {
			options.Defaults.PHPVersion = version
		}
	}

	composer, err := s.commandDependency(ctx, "composer", "/services", "/usr/local/bin/composer", "--version", "--no-ansi")
	if err != nil {
		return WebsiteOptions{}, err
	}
	options.Dependencies = []DependencyOption{composer}
	options.Profiles = websiteProfileOptions()
	return options, nil
}

func (s *Service) phpRuntimeInstalled(ctx context.Context, version string) (bool, error) {
	for _, check := range [][]string{
		{"-d", "/etc/php/" + version},
		{"-x", "/usr/bin/php" + version},
		{"-f", "/lib/systemd/system/php" + version + "-fpm.service"},
	} {
		result, err := s.exec.Run(ctx, "test", check...)
		if err != nil {
			return false, err
		}
		if result.ExitCode != 0 {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) commandDependency(ctx context.Context, label, manageURL, command string, args ...string) (DependencyOption, error) {
	status := DependencyOption{Name: label, ManageURL: manageURL}
	result, err := s.exec.Run(ctx, command, args...)
	if commandNotFound(err) {
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("check %s: %w", label, err)
	}
	if result.ExitCode == 0 {
		status.Installed = true
		status.Version = parseDependencyVersion(label, result.Stdout)
	}
	return status, nil
}

func commandNotFound(err error) bool {
	return errors.Is(err, osexec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}

func parseDependencyVersion(name, output string) string {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) == 0 {
		return ""
	}
	if name == "composer" && len(fields) >= 3 && fields[0] == "Composer" && fields[1] == "version" {
		return fields[2]
	}
	return strings.TrimPrefix(fields[0], "v")
}

func (s *Service) validateRuntimeRequirements(ctx context.Context, phpVersion string, profile Profile) error {
	if profile.Template != "static" {
		installed, err := s.phpRuntimeInstalled(ctx, phpVersion)
		if err != nil {
			return fmt.Errorf("check PHP %s: %w", phpVersion, err)
		}
		if !installed {
			return model.NewValidationError("PHP " + phpVersion + " is not installed; install it from /php")
		}
	}
	if profile.RequiresComposer {
		status, err := s.commandDependency(ctx, "composer", "/services", "/usr/local/bin/composer", "--version", "--no-ansi")
		if err != nil {
			return err
		}
		if !status.Installed {
			return model.NewValidationError("Composer is not installed; install it from /services")
		}
	}
	return nil
}

// Create creates a new website record and queues it for provisioning.
func (s *Service) Create(ctx context.Context, req CreateRequest) (model.Website, error) {
	req.Domain = strings.TrimSpace(strings.ToLower(req.Domain))
	if req.Domain == "" {
		return model.Website{}, model.NewValidationError("domain is required")
	}
	if strings.Contains(req.Domain, " ") || !domainRegex.MatchString(req.Domain) {
		return model.Website{}, model.NewValidationError("invalid domain name")
	}

	if req.Template != "static" && req.AppType != "static" && req.PHPVersion == "" {
		req.PHPVersion = "8.2"
	}
	profile, err := ResolveProfile(req)
	if err != nil {
		return model.Website{}, err
	}
	if err := s.validateRuntimeRequirements(ctx, req.PHPVersion, profile); err != nil {
		return model.Website{}, err
	}

	webUser := DomainToUser(req.Domain)
	homeDir := "/home/" + webUser
	docRoot := homeDir + "/" + profile.RelativeDocumentRoot

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	websiteID := ulid.Make().String()
	domainID := ulid.Make().String()

	w := model.Website{
		ID:               websiteID,
		Domain:           req.Domain,
		AppType:          profile.AppType,
		PHPVersion:       req.PHPVersion,
		NodeVersion:      profile.NodeVersion,
		DocumentRoot:     docRoot,
		WebUser:          webUser,
		Status:           "pending",
		SSLEnabled:       false,
		Framework:        profile.Framework,
		FrameworkVersion: profile.FrameworkVersion,
		FrontendStack:    profile.FrontendStack,
		InertiaAdapter:   profile.InertiaAdapter,
		ProjectVariant:   profile.ProjectVariant,
		SetupMode:        profile.SetupMode,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Website{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO websites (id, domain, app_type, php_version, node_version, document_root, web_user, status, ssl_enabled,
		 framework, framework_version, frontend_stack, inertia_adapter, project_variant, setup_mode, provision_stage, provision_log,
		 created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, ?)`,
		w.ID, w.Domain, w.AppType, nullableString(w.PHPVersion),
		w.NodeVersion,
		w.DocumentRoot, w.WebUser, w.Status, boolToInt(w.SSLEnabled),
		w.Framework, w.FrameworkVersion, w.FrontendStack, w.InertiaAdapter, w.ProjectVariant, w.SetupMode,
		req.CreatedBy, nowStr, nowStr,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.Website{}, model.NewValidationError("domain already exists")
		}
		return model.Website{}, fmt.Errorf("insert website: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO domains (id, website_id, name, type, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		domainID, w.ID, w.Domain, "primary", nowStr,
	)
	if err != nil {
		return model.Website{}, fmt.Errorf("insert primary domain: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Website{}, fmt.Errorf("commit: %w", err)
	}

	w.Domains = []model.Domain{
		{ID: domainID, WebsiteID: w.ID, Name: w.Domain, Type: "primary", CreatedAt: now},
	}

	// Queue provisioning in background.
	if s.prov != nil {
		if err := s.prov.Queue(ctx, w.ID); err != nil {
			s.markProvisioningQueueFailed(w.ID, err)
			return model.Website{}, fmt.Errorf("queue website provisioning: %w", err)
		}
	}

	return w, nil
}

// Get returns a website by ID, including its associated domains.
func (s *Service) Get(ctx context.Context, id string) (model.Website, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, domain, app_type, php_version, node_version, document_root, web_user,
		        status, error_message, ssl_enabled, framework, framework_version, frontend_stack,
		        inertia_adapter, project_variant, setup_mode, provision_stage, provision_log,
		        nginx_profile, created_by, created_at, updated_at
		 FROM websites WHERE id = ?`, id)

	w, err := scanWebsite(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.Website{}, model.ErrNotFound
		}
		return model.Website{}, fmt.Errorf("get website: %w", err)
	}

	if w.CreatedBy != "" {
		// Owner email is display-only; a missing user row is not an error.
		_ = s.db.QueryRowContext(ctx,
			`SELECT email FROM users WHERE id = ?`, w.CreatedBy).Scan(&w.OwnerEmail)
	}

	domains, err := s.getDomains(ctx, id)
	if err != nil {
		return model.Website{}, err
	}
	w.Domains = domains

	return w, nil
}

// TransferOwnership assigns the website to another panel user. Admin action.
func (s *Service) TransferOwnership(ctx context.Context, websiteID, userID string) (model.Website, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&exists); err != nil {
		return model.Website{}, fmt.Errorf("check target user: %w", err)
	}
	if exists == 0 {
		return model.Website{}, model.NewValidationError("target user not found")
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE websites SET created_by = ?, updated_at = ? WHERE id = ?`,
		userID, time.Now().UTC().Format(time.RFC3339), websiteID)
	if err != nil {
		return model.Website{}, fmt.Errorf("transfer website ownership: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.Website{}, model.ErrNotFound
	}
	return s.Get(ctx, websiteID)
}

// GetByWebUser returns the website operated by the given web system user,
// used by the website-scoped terminal guard. Domains are not loaded.
func (s *Service) GetByWebUser(ctx context.Context, webUser string) (model.Website, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, domain, app_type, php_version, node_version, document_root, web_user,
		        status, error_message, ssl_enabled, framework, framework_version, frontend_stack,
		        inertia_adapter, project_variant, setup_mode, provision_stage, provision_log,
		        nginx_profile, created_by, created_at, updated_at
		 FROM websites WHERE web_user = ?`, webUser)

	w, err := scanWebsite(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.Website{}, model.ErrNotFound
		}
		return model.Website{}, fmt.Errorf("get website by web user: %w", err)
	}
	return w, nil
}

// List returns all websites ordered by created_at DESC.
func (s *Service) List(ctx context.Context) ([]model.Website, error) {
	return s.listWhere(ctx, "", nil)
}

// ListByOwner returns the websites created by the given panel user.
func (s *Service) ListByOwner(ctx context.Context, userID string) ([]model.Website, error) {
	return s.listWhere(ctx, " WHERE created_by = ?", []any{userID})
}

func (s *Service) listWhere(ctx context.Context, where string, args []any) ([]model.Website, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, domain, app_type, php_version, node_version, document_root, web_user,
		        status, error_message, ssl_enabled, framework, framework_version, frontend_stack,
		        inertia_adapter, project_variant, setup_mode, provision_stage, provision_log,
		        nginx_profile, created_by, created_at, updated_at
		 FROM websites`+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list websites: %w", err)
	}
	defer rows.Close()

	var websites []model.Website
	for rows.Next() {
		w, err := scanWebsiteRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan website: %w", err)
		}
		websites = append(websites, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close website rows: %w", err)
	}

	// SQLite is configured with a single connection. Finish and close the
	// website query before loading related domains so the nested queries can
	// acquire that connection.
	for i := range websites {
		domains, err := s.getDomains(ctx, websites[i].ID)
		if err != nil {
			return nil, err
		}
		websites[i].Domains = domains
	}

	return websites, nil
}

// Update updates a website's mutable fields.
func (s *Service) Update(ctx context.Context, id string, req UpdateRequest) error {
	unlock := s.mutations.Lock(id)
	defer unlock()
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	if req.PHPVersion != nil {
		w.PHPVersion = *req.PHPVersion
	}
	if req.DocumentRoot != nil {
		w.DocumentRoot = *req.DocumentRoot
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE websites SET php_version = ?, document_root = ?, updated_at = ? WHERE id = ?`,
		nullableString(w.PHPVersion), w.DocumentRoot, now, id,
	)
	if err != nil {
		return fmt.Errorf("update website: %w", err)
	}

	return nil
}

// Delete removes a website record and all of its managed files.
func (s *Service) Delete(ctx context.Context, id string) error {
	unlock := s.mutations.Lock(id)
	defer unlock()
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !safeDomainComponent(w.Domain) {
		return fmt.Errorf("delete website: unsafe stored domain %q", w.Domain)
	}
	if !webUserRegex.MatchString(w.WebUser) || filepath.Base(w.WebUser) != w.WebUser || w.WebUser != DomainToUser(w.Domain) {
		return fmt.Errorf("delete website: unsafe stored web user %q", w.WebUser)
	}
	if w.PHPVersion != "" && !phpVersionRegex.MatchString(w.PHPVersion) {
		return fmt.Errorf("delete website: unsafe stored PHP version %q", w.PHPVersion)
	}
	sslDomains, err := s.sslDomains(ctx, id)
	if err != nil {
		return err
	}
	for _, domain := range sslDomains {
		if !safeDomainComponent(domain) {
			return fmt.Errorf("delete website: unsafe stored certificate domain %q", domain)
		}
	}
	for _, domain := range sslDomains {
		for _, path := range []string{
			filepath.Join("/etc/nginx/sites-enabled", domain+".ssl"),
			filepath.Join("/etc/nginx/sites-enabled", domain+".ssl.suspended"),
			filepath.Join("/etc/nginx/sites-available", domain+".ssl"),
		} {
			if err := s.runSudoOK(ctx, "rm", "-f", path); err != nil {
				return fmt.Errorf("delete website SSL config: %w", err)
			}
		}
		if err := s.runSudoOK(ctx, "rm", "-rf", filepath.Join("/etc/jenderal/ssl", domain)); err != nil {
			return fmt.Errorf("delete website certificate: %w", err)
		}
	}

	// Remove nginx config.
	confPath := filepath.Join("/etc/nginx/sites-available", w.Domain)
	for _, path := range []string{
		confPath,
		filepath.Join("/etc/nginx/sites-enabled", w.Domain),
		filepath.Join("/etc/nginx/sites-enabled", w.Domain+".suspended"),
		confPath + ".suspended",
	} {
		if err := s.runSudoOK(ctx, "rm", "-f", path); err != nil {
			return fmt.Errorf("delete website nginx config: %w", err)
		}
	}

	// Remove FPM pool config.
	if w.PHPVersion != "" {
		poolPath := filepath.Join("/etc/php", w.PHPVersion, "fpm/pool.d", w.Domain+".conf")
		if err := s.runSudoOK(ctx, "rm", "-f", poolPath); err != nil {
			return fmt.Errorf("delete website PHP-FPM config: %w", err)
		}
	}

	// Remove the website user home, including all application files.
	homeDir := filepath.Join("/home", w.WebUser)
	if err := s.runSudoOK(ctx, "rm", "-rf", homeDir); err != nil {
		return fmt.Errorf("delete website files: %w", err)
	}

	// Remove the system user if it still exists, keeping retries idempotent.
	userResult, err := s.exec.RunSudo(ctx, "id", "-u", w.WebUser)
	if err != nil {
		return fmt.Errorf("check website user: %w", err)
	}
	if userResult.ExitCode == 0 {
		if err := s.runSudoOK(ctx, "userdel", w.WebUser); err != nil {
			return fmt.Errorf("delete website user: %w", err)
		}
	} else if userResult.ExitCode != 1 {
		return fmt.Errorf("check website user: %s", strings.TrimSpace(userResult.Stderr))
	}

	// Reload nginx.
	if err := s.reloadNginx(ctx); err != nil {
		return fmt.Errorf("delete website: %w", err)
	}

	// Restart PHP-FPM if applicable.
	if w.PHPVersion != "" {
		if err := s.runSudoOK(ctx, "systemctl", "restart", "php"+w.PHPVersion+"-fpm"); err != nil {
			return fmt.Errorf("restart PHP-FPM after deleting website: %w", err)
		}
	}

	// Delete DB records (domains cascade).
	_, err = s.db.ExecContext(ctx, `DELETE FROM websites WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete website: %w", err)
	}

	return nil
}

// Suspend suspends a website by renaming its nginx config.
func (s *Service) Suspend(ctx context.Context, id string) error {
	unlock := s.mutations.Lock(id)
	defer unlock()
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if w.Status == "suspended" {
		return model.NewValidationError("website is already suspended")
	}

	if err := s.moveWebsiteConfigs(ctx, w, true); err != nil {
		return fmt.Errorf("suspend website: %w", err)
	}
	if err := s.reloadNginx(ctx); err != nil {
		s.restoreWebsiteConfigs(w, true)
		return fmt.Errorf("suspend website: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE websites SET status = ?, updated_at = ? WHERE id = ?`,
		"suspended", now, id,
	)
	if err != nil {
		s.restoreWebsiteConfigs(w, true)
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

// Enable re-enables a suspended website.
func (s *Service) Enable(ctx context.Context, id string) error {
	unlock := s.mutations.Lock(id)
	defer unlock()
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if w.Status != "suspended" {
		return model.NewValidationError("website is not suspended")
	}

	if err := s.moveWebsiteConfigs(ctx, w, false); err != nil {
		return fmt.Errorf("enable website: %w", err)
	}
	if err := s.reloadNginx(ctx); err != nil {
		s.restoreWebsiteConfigs(w, false)
		return fmt.Errorf("enable website: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE websites SET status = ?, updated_at = ? WHERE id = ?`,
		"active", now, id,
	)
	if err != nil {
		s.restoreWebsiteConfigs(w, false)
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

// Retry resets a failed website to pending and re-queues provisioning.
func (s *Service) Retry(ctx context.Context, id string) error {
	unlock := s.mutations.Lock(id)
	defer unlock()
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if w.Status != "failed" {
		return model.NewValidationError("only failed websites can be retried")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE websites SET status = ?, error_message = NULL, provision_stage = ?, updated_at = ? WHERE id = ?`,
		"pending", "queued", now, id,
	)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	if s.prov != nil {
		if err := s.prov.Queue(ctx, id); err != nil {
			s.markProvisioningQueueFailed(id, err)
			return fmt.Errorf("queue website provisioning: %w", err)
		}
	}

	return nil
}

func (s *Service) markProvisioningQueueFailed(websiteID string, queueErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx,
		`UPDATE websites SET status = 'failed', error_message = ?, provision_stage = 'queue failed', updated_at = ? WHERE id = ?`,
		limitProvisionError("queue provisioning failed: "+queueErr.Error()), time.Now().UTC().Format(time.RFC3339), websiteID)
}

// GetConfig returns the Nginx vhost configuration for a website.
func (s *Service) GetConfig(ctx context.Context, id string) (string, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}

	confPath := "/etc/nginx/sites-available/" + w.Domain
	result, err := s.exec.RunSudo(ctx, "cat", confPath)
	if err != nil {
		return "", fmt.Errorf("read vhost config: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read vhost config: %s", strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// SaveConfig saves an Nginx vhost configuration with validation and rollback.
func (s *Service) SaveConfig(ctx context.Context, id, content string) error {
	unlock := s.mutations.Lock(id)
	defer unlock()
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	confPath := "/etc/nginx/sites-available/" + w.Domain
	bakPath := confPath + ".bak"
	tmpFile, err := os.CreateTemp("", "jenderal_website_vhost_*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}

	// Backup current config.
	_, _ = s.exec.RunSudo(ctx, "cp", confPath, bakPath)

	// Copy temp to live.
	result, err := s.exec.RunSudo(ctx, "cp", tmpPath, confPath)
	if err != nil {
		return fmt.Errorf("write vhost config: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write vhost config: %s", strings.TrimSpace(result.Stderr))
	}

	// Validate.
	testResult, err := s.exec.RunSudo(ctx, "nginx", "-t")
	if err != nil {
		return fmt.Errorf("test nginx config: %w", err)
	}
	if testResult.ExitCode != 0 {
		// Restore from backup.
		_, _ = s.exec.RunSudo(ctx, "cp", bakPath, confPath)
		return model.NewDomainError("NGINX_CONFIG_INVALID",
			"nginx configuration is invalid: "+strings.TrimSpace(testResult.Stderr), nil)
	}

	// Reload nginx.
	_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")

	return nil
}

// SetNginxProfile sets the per-website nginx template override and regenerates
// the vhost config from the selected template. An empty profile returns to
// automatic selection based on the app type.
func (s *Service) SetNginxProfile(ctx context.Context, id, profile string) (model.Website, error) {
	if !IsValidNginxProfile(profile) {
		return model.Website{}, model.NewValidationError("unsupported nginx profile: "+profile)
	}

	unlock := s.mutations.Lock(id)
	defer unlock()

	result, err := s.db.ExecContext(ctx,
		`UPDATE websites SET nginx_profile = ?, updated_at = ? WHERE id = ?`,
		profile, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return model.Website{}, fmt.Errorf("update nginx profile: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return model.Website{}, model.ErrNotFound
	}

	w, err := s.Get(ctx, id)
	if err != nil {
		return model.Website{}, err
	}

	if err := s.regenerateConfig(ctx, w, ""); err != nil {
		return model.Website{}, fmt.Errorf("regenerate vhost from template: %w", err)
	}

	return w, nil
}

// AddDomain adds an alias or subdomain to a website and regenerates the nginx config.
func (s *Service) AddDomain(ctx context.Context, websiteID, name, domainType string) error {
	unlock := s.mutations.Lock(websiteID)
	defer unlock()
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return model.NewValidationError("domain name is required")
	}
	if !domainRegex.MatchString(name) {
		return model.NewValidationError("invalid domain name")
	}

	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}

	if domainType == "" {
		domainType = "alias"
	}

	domainID := ulid.Make().String()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO domains (id, website_id, name, type, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		domainID, websiteID, name, domainType, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.NewValidationError("domain already exists")
		}
		return fmt.Errorf("insert domain: %w", err)
	}

	// Regenerate nginx config.
	if err := s.regenerateConfig(ctx, w, name); err != nil {
		return err
	}

	return nil
}

// RemoveDomain removes a domain from a website. Primary domains cannot be removed.
func (s *Service) RemoveDomain(ctx context.Context, websiteID, domainID string) error {
	unlock := s.mutations.Lock(websiteID)
	defer unlock()
	// Check domain type.
	var domainName, domainType string
	err := s.db.QueryRowContext(ctx,
		`SELECT name, type FROM domains WHERE id = ? AND website_id = ?`,
		domainID, websiteID,
	).Scan(&domainName, &domainType)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrNotFound
		}
		return fmt.Errorf("get domain: %w", err)
	}

	if domainType == "primary" {
		return model.NewValidationError("cannot remove primary domain")
	}
	var certificateCount int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ssl_certificates WHERE website_id = ? AND domain = ?`,
		websiteID, domainName,
	).Scan(&certificateCount); err != nil {
		return fmt.Errorf("check domain SSL certificate: %w", err)
	}
	if certificateCount > 0 {
		return model.NewValidationError("delete the domain's SSL certificate before removing the domain")
	}

	_, err = s.db.ExecContext(ctx,
		`DELETE FROM domains WHERE id = ? AND website_id = ?`,
		domainID, websiteID,
	)
	if err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}

	// Regenerate nginx config.
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}
	if err := s.regenerateConfig(ctx, w, ""); err != nil {
		return err
	}

	return nil
}

// Count returns the total number of websites for dashboard use.
func (s *Service) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM websites`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count websites: %w", err)
	}
	return count, nil
}

// GetLogs returns the last N lines of a website-specific log file.
// logType can be "access" or "error".
func (s *Service) GetLogs(ctx context.Context, id, logType string, lines int) (string, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}

	if logType != "access" && logType != "error" {
		return "", model.NewValidationError("log_type must be access or error")
	}

	logDir := "/home/" + w.WebUser + "/logs"
	logPath := logDir + "/" + logType + ".log"

	result, err := s.exec.RunSudo(ctx, "tail", "-n", strconv.Itoa(lines), logPath)
	if err != nil {
		return "", fmt.Errorf("read %s log: %w", logType, err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read %s log: %s", logType, strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// regenerateConfig rebuilds the nginx vhost config and reloads nginx.
func (s *Service) regenerateConfig(ctx context.Context, w model.Website, _ string) error {
	// Build aliases from all non-primary domains.
	var aliases []string
	for _, d := range w.Domains {
		if d.Type != "primary" {
			aliases = append(aliases, d.Name)
		}
	}

	// Re-fetch domains to include any newly added ones.
	domains, err := s.getDomains(ctx, w.ID)
	if err != nil {
		return err
	}
	aliases = nil
	for _, d := range domains {
		if d.Type != "primary" {
			aliases = append(aliases, d.Name)
		}
	}

	logDir := "/home/" + w.WebUser + "/logs"
	redirectDomains, err := s.activeSSLDomains(ctx, w.ID)
	if err != nil {
		return err
	}

	vhostData := VhostData{
		Domain:            w.Domain,
		Aliases:           strings.Join(aliases, " "),
		DocumentRoot:      w.DocumentRoot,
		ACMEChallengeRoot: DefaultACMEChallengeRoot,
		LogDir:            logDir,
		PHPVersion:        w.PHPVersion,
		AppType:           w.AppType,
		Profile:           NginxProfileForWebsite(w),
		IPv6:              s.ipv6Available(),
		RedirectDomains:   redirectDomains,
		SecurityInclude:   "/etc/nginx/jenderal/security/sites/" + w.ID + ".conf",
	}

	content, err := RenderVhost(vhostData)
	if err != nil {
		return fmt.Errorf("render vhost: %w", err)
	}

	confPath := "/etc/nginx/sites-available/" + w.Domain
	tmpFile, err := os.CreateTemp("", "jenderal_website_regen_*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}

	result, err := s.exec.RunSudo(ctx, "cp", tmpPath, confPath)
	if err != nil {
		return fmt.Errorf("write vhost config: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write vhost config: %s", strings.TrimSpace(result.Stderr))
	}

	_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")

	return nil
}

func (s *Service) activeSSLDomains(ctx context.Context, websiteID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT domain FROM ssl_certificates WHERE website_id = ? AND status = 'active' ORDER BY domain`,
		websiteID,
	)
	if err != nil {
		return nil, fmt.Errorf("get active SSL domains: %w", err)
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, fmt.Errorf("scan active SSL domain: %w", err)
		}
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get active SSL domains: %w", err)
	}
	return domains, nil
}

func (s *Service) sslDomains(ctx context.Context, websiteID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT sc.domain, CASE WHEN d.id IS NULL THEN 0 ELSE 1 END
		 FROM ssl_certificates AS sc
		 LEFT JOIN domains AS d ON d.website_id = sc.website_id AND d.name = sc.domain
		 WHERE sc.website_id = ? ORDER BY sc.domain`,
		websiteID,
	)
	if err != nil {
		return nil, fmt.Errorf("get website SSL domains: %w", err)
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var domain string
		var registered int
		if err := rows.Scan(&domain, &registered); err != nil {
			return nil, fmt.Errorf("scan website SSL domain: %w", err)
		}
		if registered == 0 {
			return nil, fmt.Errorf("delete website: certificate domain %q is not registered to the website", domain)
		}
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get website SSL domains: %w", err)
	}
	return domains, nil
}

func (s *Service) moveWebsiteConfigs(ctx context.Context, w model.Website, suspend bool) error {
	sslDomains, err := s.activeSSLDomains(ctx, w.ID)
	if err != nil {
		return err
	}
	paths := []string{"/etc/nginx/sites-enabled/" + w.Domain}
	for _, domain := range sslDomains {
		paths = append(paths, "/etc/nginx/sites-enabled/"+domain+".ssl")
	}

	moved := make([][2]string, 0, len(paths))
	for _, path := range paths {
		from, to := path, path+".suspended"
		if !suspend {
			from, to = to, from
		}
		result, moveErr := s.exec.RunSudo(ctx, "mv", from, to)
		if moveErr != nil || result.ExitCode != 0 {
			for i := len(moved) - 1; i >= 0; i-- {
				_, _ = s.exec.RunSudo(ctx, "mv", moved[i][1], moved[i][0])
			}
			if moveErr != nil {
				return moveErr
			}
			return fmt.Errorf("move %s: %s", from, strings.TrimSpace(result.Stderr))
		}
		moved = append(moved, [2]string{from, to})
	}
	return nil
}

func (s *Service) reloadNginx(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("reload nginx: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (s *Service) runSudoOK(ctx context.Context, name string, args ...string) error {
	result, err := s.exec.RunSudo(ctx, name, args...)
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

func safeDomainComponent(domain string) bool {
	return domainRegex.MatchString(domain) && filepath.Base(domain) == domain && domain != "." && domain != ".."
}

func (s *Service) restoreWebsiteConfigs(w model.Website, previousSuspend bool) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_ = s.moveWebsiteConfigs(cleanupCtx, w, !previousSuspend)
	_ = s.reloadNginx(cleanupCtx)
}

// getDomains returns all domains for a website.
func (s *Service) getDomains(ctx context.Context, websiteID string) ([]model.Domain, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, name, type, created_at FROM domains WHERE website_id = ? ORDER BY created_at`,
		websiteID,
	)
	if err != nil {
		return nil, fmt.Errorf("get domains: %w", err)
	}
	defer rows.Close()

	var domains []model.Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("scan domain: %w", err)
		}
		domains = append(domains, d)
	}

	return domains, rows.Err()
}

// scanner is an interface satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanWebsite scans a single website row from *sql.Row.
func scanWebsite(row *sql.Row) (model.Website, error) {
	var w model.Website
	var phpVersion, errorMessage, framework, frameworkVersion, frontendStack, inertiaAdapter, projectVariant, setupMode, provisionStage, provisionLog sql.NullString
	var sslEnabled int
	var createdStr, updatedStr string

	err := row.Scan(
		&w.ID, &w.Domain, &w.AppType, &phpVersion,
		&w.NodeVersion, &w.DocumentRoot, &w.WebUser, &w.Status, &errorMessage,
		&sslEnabled, &framework, &frameworkVersion, &frontendStack, &inertiaAdapter,
		&projectVariant, &setupMode, &provisionStage, &provisionLog,
		&w.NginxProfile, &w.CreatedBy, &createdStr, &updatedStr,
	)
	if err != nil {
		return w, err
	}

	w.PHPVersion = phpVersion.String
	w.ErrorMessage = errorMessage.String
	assignProfileFields(&w, framework, frameworkVersion, frontendStack, inertiaAdapter, projectVariant, setupMode, provisionStage, provisionLog)
	w.SSLEnabled = sslEnabled == 1
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return w, nil
}

// scanWebsiteRows scans a single website row from *sql.Rows.
func scanWebsiteRows(rows *sql.Rows) (model.Website, error) {
	var w model.Website
	var phpVersion, errorMessage, framework, frameworkVersion, frontendStack, inertiaAdapter, projectVariant, setupMode, provisionStage, provisionLog sql.NullString
	var sslEnabled int
	var createdStr, updatedStr string

	err := rows.Scan(
		&w.ID, &w.Domain, &w.AppType, &phpVersion,
		&w.NodeVersion, &w.DocumentRoot, &w.WebUser, &w.Status, &errorMessage,
		&sslEnabled, &framework, &frameworkVersion, &frontendStack, &inertiaAdapter,
		&projectVariant, &setupMode, &provisionStage, &provisionLog,
		&w.NginxProfile, &w.CreatedBy, &createdStr, &updatedStr,
	)
	if err != nil {
		return w, err
	}

	w.PHPVersion = phpVersion.String
	w.ErrorMessage = errorMessage.String
	assignProfileFields(&w, framework, frameworkVersion, frontendStack, inertiaAdapter, projectVariant, setupMode, provisionStage, provisionLog)
	w.SSLEnabled = sslEnabled == 1
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return w, nil
}

func assignProfileFields(w *model.Website, framework, frameworkVersion, frontendStack, inertiaAdapter, projectVariant, setupMode, provisionStage, provisionLog sql.NullString) {
	w.Framework = valueOr(framework.String, "none")
	w.FrameworkVersion = frameworkVersion.String
	w.FrontendStack = frontendStack.String
	w.InertiaAdapter = inertiaAdapter.String
	w.ProjectVariant = valueOr(projectVariant.String, "empty")
	w.SetupMode = valueOr(setupMode.String, SetupConfigOnly)
	w.ProvisionStage = provisionStage.String
	w.ProvisionLog = provisionLog.String
}

// scanDomain scans a single domain row from *sql.Rows.
func scanDomain(rows *sql.Rows) (model.Domain, error) {
	var d model.Domain
	var createdStr string

	err := rows.Scan(&d.ID, &d.WebsiteID, &d.Name, &d.Type, &createdStr)
	if err != nil {
		return d, err
	}

	d.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

	return d, nil
}

// boolToInt converts a bool to 0 or 1 for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullableString returns nil if s is empty, otherwise returns s.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
