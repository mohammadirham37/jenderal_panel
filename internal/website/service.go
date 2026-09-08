package website

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	nginxconfig "github.com/mohammadirham37/jenderal_panel/internal/nginx"
)

// domainRegex validates domain names: alphanumeric, hyphens, dots.
var domainRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?)*$`)

// CreateRequest holds the data for creating a new website.
type CreateRequest struct {
	Domain     string `json:"domain"`
	AppType    string `json:"app_type"`
	PHPVersion string `json:"php_version"`
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
}

// NewService creates a new website management service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc, ipv6Available: nginxconfig.IPv6Available}
}

// SetProvisioner sets the provisioner after creation to break circular
// dependency between Service and Provisioner.
func (s *Service) SetProvisioner(p *Provisioner) {
	s.prov = p
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

	if req.AppType == "" {
		req.AppType = "php"
	}
	validTypes := map[string]bool{"php": true, "laravel": true, "static": true}
	if !validTypes[req.AppType] {
		return model.Website{}, model.NewValidationError("app_type must be php, laravel, or static")
	}

	if req.AppType != "static" && req.PHPVersion == "" {
		req.PHPVersion = "8.2"
	}

	webUser := DomainToUser(req.Domain)
	homeDir := "/home/" + webUser
	docRoot := homeDir + "/public"

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	websiteID := ulid.Make().String()
	domainID := ulid.Make().String()

	w := model.Website{
		ID:           websiteID,
		Domain:       req.Domain,
		AppType:      req.AppType,
		PHPVersion:   req.PHPVersion,
		DocumentRoot: docRoot,
		WebUser:      webUser,
		Status:       "pending",
		SSLEnabled:   false,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Website{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO websites (id, domain, app_type, php_version, document_root, web_user, status, ssl_enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Domain, w.AppType, nullableString(w.PHPVersion),
		w.DocumentRoot, w.WebUser, w.Status, boolToInt(w.SSLEnabled),
		nowStr, nowStr,
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
		s.prov.Queue(w.ID)
	}

	return w, nil
}

// Get returns a website by ID, including its associated domains.
func (s *Service) Get(ctx context.Context, id string) (model.Website, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, domain, app_type, php_version, document_root, web_user,
		        status, error_message, ssl_enabled, created_at, updated_at
		 FROM websites WHERE id = ?`, id)

	w, err := scanWebsite(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.Website{}, model.ErrNotFound
		}
		return model.Website{}, fmt.Errorf("get website: %w", err)
	}

	domains, err := s.getDomains(ctx, id)
	if err != nil {
		return model.Website{}, err
	}
	w.Domains = domains

	return w, nil
}

// List returns all websites ordered by created_at DESC.
func (s *Service) List(ctx context.Context) ([]model.Website, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, domain, app_type, php_version, document_root, web_user,
		        status, error_message, ssl_enabled, created_at, updated_at
		 FROM websites ORDER BY created_at DESC`)
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

// Delete removes a website record and optionally its files.
func (s *Service) Delete(ctx context.Context, id string, removeFiles bool) error {
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	sslDomains, err := s.sslDomains(ctx, id)
	if err != nil {
		return err
	}
	for _, domain := range sslDomains {
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", "/etc/nginx/sites-enabled/"+domain+".ssl")
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", "/etc/nginx/sites-enabled/"+domain+".ssl.suspended")
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", "/etc/nginx/sites-available/"+domain+".ssl")
		_, _ = s.exec.RunSudo(ctx, "rm", "-rf", "/etc/jenderal/ssl/"+domain)
	}

	// Remove nginx config.
	confPath := "/etc/nginx/sites-available/" + w.Domain
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", confPath)
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", "/etc/nginx/sites-enabled/"+w.Domain)
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", "/etc/nginx/sites-enabled/"+w.Domain+".suspended")
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", confPath+".suspended")

	// Remove FPM pool config.
	if w.PHPVersion != "" {
		poolPath := "/etc/php/" + w.PHPVersion + "/fpm/pool.d/" + w.Domain + ".conf"
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", poolPath)
	}

	// Optionally remove user home directory.
	if removeFiles {
		homeDir := "/home/" + w.WebUser
		_, _ = s.exec.RunSudo(ctx, "rm", "-rf", homeDir)
	}

	// Remove system user.
	_, _ = s.exec.RunSudo(ctx, "userdel", w.WebUser)

	// Reload nginx.
	_, _ = s.exec.RunSudo(ctx, "systemctl", "reload", "nginx")

	// Restart PHP-FPM if applicable.
	if w.PHPVersion != "" {
		_, _ = s.exec.RunSudo(ctx, "systemctl", "restart", "php"+w.PHPVersion+"-fpm")
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
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if w.Status != "failed" {
		return model.NewValidationError("only failed websites can be retried")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE websites SET status = ?, error_message = NULL, updated_at = ? WHERE id = ?`,
		"pending", now, id,
	)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	if s.prov != nil {
		s.prov.Queue(id)
	}

	return nil
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
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	confPath := "/etc/nginx/sites-available/" + w.Domain
	bakPath := confPath + ".bak"
	tmpPath := "/tmp/jenderal_website_vhost.tmp"

	// Write to temp file.
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
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

// AddDomain adds an alias or subdomain to a website and regenerates the nginx config.
func (s *Service) AddDomain(ctx context.Context, websiteID, name, domainType string) error {
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
		IPv6:              s.ipv6Available(),
		RedirectDomains:   redirectDomains,
	}

	content, err := RenderVhost(vhostData)
	if err != nil {
		return fmt.Errorf("render vhost: %w", err)
	}

	confPath := "/etc/nginx/sites-available/" + w.Domain
	tmpPath := "/tmp/jenderal_website_regen.tmp"

	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
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
		`SELECT domain FROM ssl_certificates WHERE website_id = ? AND status = 'active' ORDER BY created_at`,
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
		`SELECT DISTINCT domain FROM ssl_certificates WHERE website_id = ? ORDER BY domain`,
		websiteID,
	)
	if err != nil {
		return nil, fmt.Errorf("get website SSL domains: %w", err)
	}
	defer rows.Close()

	var domains []string
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, fmt.Errorf("scan website SSL domain: %w", err)
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
	var phpVersion, errorMessage sql.NullString
	var sslEnabled int
	var createdStr, updatedStr string

	err := row.Scan(
		&w.ID, &w.Domain, &w.AppType, &phpVersion,
		&w.DocumentRoot, &w.WebUser, &w.Status, &errorMessage,
		&sslEnabled, &createdStr, &updatedStr,
	)
	if err != nil {
		return w, err
	}

	w.PHPVersion = phpVersion.String
	w.ErrorMessage = errorMessage.String
	w.SSLEnabled = sslEnabled == 1
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return w, nil
}

// scanWebsiteRows scans a single website row from *sql.Rows.
func scanWebsiteRows(rows *sql.Rows) (model.Website, error) {
	var w model.Website
	var phpVersion, errorMessage sql.NullString
	var sslEnabled int
	var createdStr, updatedStr string

	err := rows.Scan(
		&w.ID, &w.Domain, &w.AppType, &phpVersion,
		&w.DocumentRoot, &w.WebUser, &w.Status, &errorMessage,
		&sslEnabled, &createdStr, &updatedStr,
	)
	if err != nil {
		return w, err
	}

	w.PHPVersion = phpVersion.String
	w.ErrorMessage = errorMessage.String
	w.SSLEnabled = sslEnabled == 1
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return w, nil
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
