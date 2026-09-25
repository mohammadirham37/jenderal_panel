// Package websitestaging clones a website into a new staging site: it
// provisions a fresh config-only site, copies the document root, and when
// the source has a database it dumps it into a new staging database and
// rewrites the app configuration (wp-config.php / .env) plus WordPress URLs.
package websitestaging

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/dbdump"
	"github.com/mohammadirham37/jenderal_panel/internal/dbmanager"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
	"github.com/mohammadirham37/jenderal_panel/internal/website"
)

// Clone lifecycle statuses.
const (
	StatusWaitingProvision = "waiting_provision"
	StatusCopying          = "copying"
	StatusCompleted        = "completed"
	StatusFailed           = "failed"
)

// StagingClone is one clone operation record.
type StagingClone struct {
	ID              string    `json:"id"`
	SourceWebsiteID string    `json:"source_website_id"`
	TargetWebsiteID string    `json:"target_website_id"`
	TargetDomain    string    `json:"target_domain"`
	Status          string    `json:"status"`
	TaskID          string    `json:"task_id"`
	Error           string    `json:"error"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Service creates and tracks staging clones.
type Service struct {
	db       *sql.DB
	exec     executor.CommandExecutor
	websites *website.Service
	dbs      *dbmanager.Service
	tasks    *taskrunner.Runner
	audit    *audit.Service

	// Polling knobs, overridable in tests.
	provisionPollInterval time.Duration
	provisionTimeout      time.Duration
	taskPollInterval      time.Duration
	taskTimeout           time.Duration
}

// NewService creates a new websitestaging Service.
func NewService(db *sql.DB, exec executor.CommandExecutor, websites *website.Service,
	dbs *dbmanager.Service, tasks *taskrunner.Runner, auditSvc *audit.Service) *Service {
	return &Service{
		db:                    db,
		exec:                  exec,
		websites:              websites,
		dbs:                   dbs,
		tasks:                 tasks,
		audit:                 auditSvc,
		provisionPollInterval: 3 * time.Second,
		provisionTimeout:      15 * time.Minute,
		taskPollInterval:      5 * time.Second,
		taskTimeout:           30 * time.Minute,
	}
}

// StartClone provisions a staging site for the source website and kicks off
// the background copy. It returns immediately with the clone record.
func (s *Service) StartClone(ctx context.Context, sourceID, domain, callerID string) (StagingClone, error) {
	src, err := s.websites.Get(ctx, sourceID)
	if err != nil {
		return StagingClone{}, err
	}

	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return StagingClone{}, model.NewValidationError("staging domain is required")
	}
	if domain == strings.ToLower(src.Domain) {
		return StagingClone{}, model.NewValidationError("the staging domain must differ from the site domain")
	}

	// A config-only static site gives the clone its own system user, nginx
	// vhost and PHP pool without installing any framework into it. Static
	// sites carry no PHP version by definition.
	target, err := s.websites.Create(ctx, website.CreateRequest{
		Domain:    domain,
		Template:  "static",
		SetupMode: website.SetupConfigOnly,
		CreatedBy: callerID,
	})
	if err != nil {
		return StagingClone{}, fmt.Errorf("create staging site: %w", err)
	}

	clone, err := s.insertClone(ctx, sourceID, target)
	if err != nil {
		return StagingClone{}, err
	}

	go s.runClone(clone, src, target)

	_ = s.audit.Log(ctx, audit.LogEntry{
		UserID: callerID,
		Action: "staging_clone_start",
		Module: "websites",
		Target: src.Domain,
		Detail: "cloning to staging site " + domain,
	})

	return clone, nil
}

// ListClones returns the clone history of a website, newest first.
func (s *Service) ListClones(ctx context.Context, sourceID string) ([]StagingClone, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_website_id, target_website_id, target_domain, status, task_id, error, created_at, updated_at
		 FROM staging_clones WHERE source_website_id = ? ORDER BY created_at DESC`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query staging clones: %w", err)
	}
	defer rows.Close()

	clones := []StagingClone{}
	for rows.Next() {
		c, err := scanClone(rows.Scan)
		if err != nil {
			return nil, err
		}
		clones = append(clones, c)
	}
	return clones, rows.Err()
}

func scanClone(scan func(dest ...any) error) (StagingClone, error) {
	var c StagingClone
	var created, updated string
	if err := scan(&c.ID, &c.SourceWebsiteID, &c.TargetWebsiteID, &c.TargetDomain,
		&c.Status, &c.TaskID, &c.Error, &created, &updated); err != nil {
		return StagingClone{}, fmt.Errorf("scan staging clone: %w", err)
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return c, nil
}

func (s *Service) insertClone(ctx context.Context, sourceID string, target model.Website) (StagingClone, error) {
	now := time.Now().UTC()
	clone := StagingClone{
		ID:              ulid.Make().String(),
		SourceWebsiteID: sourceID,
		TargetWebsiteID: target.ID,
		TargetDomain:    target.Domain,
		Status:          StatusWaitingProvision,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO staging_clones (id, source_website_id, target_website_id, target_domain, status, task_id, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		clone.ID, clone.SourceWebsiteID, clone.TargetWebsiteID, clone.TargetDomain,
		clone.Status, clone.TaskID, clone.Error,
		clone.CreatedAt.Format(time.RFC3339), clone.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return StagingClone{}, fmt.Errorf("insert staging clone: %w", err)
	}
	return clone, nil
}

// updateCloneStatus writes status/error/task id on a clone row.
func (s *Service) updateCloneStatus(ctx context.Context, id, status, taskID, errText string) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE staging_clones SET status = ?, task_id = ?, error = ?, updated_at = ? WHERE id = ?`,
		status, taskID, errText, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		log.Printf("staging: update clone %s: %v", id, err)
	}
}

// runClone waits for the target site to finish provisioning, then executes
// the copy plan as a task. All outcomes are recorded on the clone row.
func (s *Service) runClone(clone StagingClone, src, target model.Website) {
	bgCtx := context.Background()

	if err := s.waitForWebsite(bgCtx, target.ID, s.provisionTimeout); err != nil {
		s.updateCloneStatus(bgCtx, clone.ID, StatusFailed, "", err.Error())
		return
	}

	plan, err := s.buildPlan(bgCtx, src, target)
	if err != nil {
		s.updateCloneStatus(bgCtx, clone.ID, StatusFailed, "", err.Error())
		return
	}

	if len(plan.commands) == 0 {
		s.updateCloneStatus(bgCtx, clone.ID, StatusCompleted, "", "")
		return
	}

	taskID := s.tasks.RunMultiple("Staging clone "+src.Domain+" → "+target.Domain, plan.commands)
	s.updateCloneStatus(bgCtx, clone.ID, StatusCopying, taskID, "")

	if err := s.waitForTask(bgCtx, taskID); err != nil {
		s.updateCloneStatus(bgCtx, clone.ID, StatusFailed, taskID, err.Error())
		return
	}
	s.updateCloneStatus(bgCtx, clone.ID, StatusCompleted, taskID, "")
}

// waitForWebsite blocks until the website reaches status "active".
func (s *Service) waitForWebsite(ctx context.Context, websiteID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		w, err := s.websites.Get(ctx, websiteID)
		if err == nil {
			switch w.Status {
			case "active":
				return nil
			case "failed", "error":
				return fmt.Errorf("staging site provisioning failed: %s", w.ErrorMessage)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for the staging site to finish provisioning")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.provisionPollInterval):
		}
	}
}

// waitForTask blocks until the task runner finishes the copy task.
func (s *Service) waitForTask(ctx context.Context, taskID string) error {
	deadline := time.Now().Add(s.taskTimeout)
	for {
		if task, ok := s.tasks.Get(taskID); ok {
			switch task.Status {
			case "completed":
				return nil
			case "failed":
				if task.Error != "" {
					return fmt.Errorf("copy task failed: %s", task.Error)
				}
				return fmt.Errorf("copy task failed")
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for the copy task")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.taskPollInterval):
		}
	}
}

// sourceApp describes what the clone plan found on the source site.
type sourceApp struct {
	IsWordPress bool
	HasEnv      bool
	HasWpPhar   bool
	DBName      string
	DBEngine    string // "mysql" or "postgresql"; empty when no database
}

// clonePlan is the outcome of planning: staging database (already created
// when a DB is involved) plus the argv command list for the task runner.
type clonePlan struct {
	commands [][]string
}

// buildPlan detects the source app kind, creates the staging database when
// needed, and returns the ordered command list.
func (s *Service) buildPlan(ctx context.Context, src, target model.Website) (clonePlan, error) {
	app, err := s.detectSourceApp(ctx, src)
	if err != nil {
		return clonePlan{}, err
	}

	commands := [][]string{
		{"cp", "-a", src.DocumentRoot + "/.", target.DocumentRoot + "/"},
		{"chown", "-R", target.WebUser + ":" + target.WebUser, target.DocumentRoot},
	}

	var stagingDBName, stagingDBUser, stagingDBPass string
	if app.DBName != "" {
		stagingDBName, stagingDBUser, stagingDBPass, err = s.createStagingDatabase(ctx, target, app.DBEngine)
		if err != nil {
			return clonePlan{}, err
		}

		dumpPath := "/var/tmp/staging-clone-" + strings.ToLower(ulid.Make().String()) + ".sql"
		dumpBin, dumpArgs, err := dbdump.DumpToFileCommand(app.DBEngine, app.DBName, dumpPath)
		if err != nil {
			return clonePlan{}, err
		}
		restoreBin, restoreArgs, err := dbdump.RestorePipelineCommand(app.DBEngine, stagingDBName, dumpPath)
		if err != nil {
			return clonePlan{}, err
		}
		commands = append(commands,
			append([]string{dumpBin}, dumpArgs...),
			append([]string{restoreBin}, restoreArgs...),
			[]string{"rm", "-f", dumpPath},
		)

		if app.IsWordPress {
			commands = append(commands, wpConfigCommands(target, stagingDBName, stagingDBUser, stagingDBPass)...)
		} else {
			commands = append(commands, envConfigCommands(target, stagingDBName, stagingDBUser, stagingDBPass)...)
		}
	}

	if app.IsWordPress && app.HasWpPhar {
		commands = append(commands, wpSearchReplaceCommands(src, target)...)
	}

	return clonePlan{commands: commands}, nil
}

// detectSourceApp inspects the source document root for WordPress markers,
// a .env file, and the database it points at.
func (s *Service) detectSourceApp(ctx context.Context, src model.Website) (sourceApp, error) {
	app := sourceApp{DBEngine: "mysql"}

	wpConfig := src.DocumentRoot + "/wp-config.php"
	exists, err := s.fileExists(ctx, wpConfig)
	if err != nil {
		return app, err
	}
	if exists {
		app.IsWordPress = true
		if name, err := s.grepFirst(ctx, `-oP`, `define\(\s*'DB_NAME',\s*'\K[^']+`, wpConfig); err == nil && name != "" {
			app.DBName = name
			app.DBEngine = "mysql"
		}
		phar := "/home/" + src.WebUser + "/.wp-cli/wp-cli.phar"
		if pharExists, err := s.fileExists(ctx, phar); err == nil && pharExists {
			app.HasWpPhar = true
		}
		return app, nil
	}

	envFile := src.DocumentRoot + "/.env"
	exists, err = s.fileExists(ctx, envFile)
	if err != nil {
		return app, err
	}
	if exists {
		app.HasEnv = true
		if name, err := s.grepFirst(ctx, `-oP`, `(?m)^DB_DATABASE=\K.*`, envFile); err == nil && name != "" {
			app.DBName = unquote(uncomment(name))
		}
		if conn, err := s.grepFirst(ctx, `-oP`, `(?m)^DB_CONNECTION=\K.*`, envFile); err == nil && conn != "" {
			switch unquote(uncomment(conn)) {
			case "pgsql", "postgresql":
				app.DBEngine = "postgresql"
			default:
				app.DBEngine = "mysql"
			}
		}
	}

	return app, nil
}

func (s *Service) fileExists(ctx context.Context, path string) (bool, error) {
	res, err := s.exec.RunSudo(ctx, "test", "-f", path)
	if err != nil {
		return false, fmt.Errorf("test %s: %w", path, err)
	}
	return res.ExitCode == 0, nil
}

// grepFirst runs grep with fixed args on a root-readable file and returns
// the first match line.
func (s *Service) grepFirst(ctx context.Context, flag, pattern, path string) (string, error) {
	res, err := s.exec.RunSudo(ctx, "grep", flag, pattern, path)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("grep found nothing in %s", path)
	}
	line := strings.TrimSpace(res.Stdout)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return line, nil
}

// uncomment strips a trailing inline comment: "value # comment" -> "value".
func uncomment(v string) string {
	if i := strings.Index(v, "#"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// unquote strips matching surrounding quotes.
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// createStagingDatabase provisions a fresh database plus a dedicated user
// for the staging site and returns their credentials.
func (s *Service) createStagingDatabase(ctx context.Context, target model.Website, engine string) (name, user, pass string, err error) {
	suffix, err := randomToken(6)
	if err != nil {
		return "", "", "", err
	}

	slug := sanitizeIdentifier(target.Domain)
	name = truncate("stg_"+slug, 50) + "_" + suffix
	user = truncate("stg_"+slug, 24) + suffix[:4]
	pass, err = randomToken(24)
	if err != nil {
		return "", "", "", err
	}

	charset := ""
	if engine == "mysql" {
		charset = "utf8mb4"
	}
	db, err := s.dbs.CreateDatabase(ctx, name, engine, charset, "")
	if err != nil {
		return "", "", "", fmt.Errorf("create staging database: %w", err)
	}
	dbUser, err := s.dbs.CreateDBUser(ctx, user, pass, engine, "")
	if err != nil {
		return "", "", "", fmt.Errorf("create staging database user: %w", err)
	}
	if err := s.dbs.GrantPrivileges(ctx, dbUser.ID, db.ID); err != nil {
		return "", "", "", fmt.Errorf("grant staging database privileges: %w", err)
	}
	return name, user, pass, nil
}

// wpConfigCommands rewrites the DB defines in the cloned wp-config.php.
func wpConfigCommands(target model.Website, dbName, dbUser, dbPass string) [][]string {
	config := target.DocumentRoot + "/wp-config.php"
	return [][]string{
		{"sed", "-E", "-i", "-e", `s|'DB_NAME',\s*'[^']*'|'DB_NAME', '` + dbName + `'|`, config},
		{"sed", "-E", "-i", "-e", `s|'DB_USER',\s*'[^']*'|'DB_USER', '` + dbUser + `'|`, config},
		{"sed", "-E", "-i", "-e", `s|'DB_PASSWORD',\s*'[^']*'|'DB_PASSWORD', '` + dbPass + `'|`, config},
	}
}

// envConfigCommands rewrites the database and APP_URL entries in .env.
func envConfigCommands(target model.Website, dbName, dbUser, dbPass string) [][]string {
	env := target.DocumentRoot + "/.env"
	return [][]string{
		{"sed", "-E", "-i",
			"-e", `s|^APP_URL=.*|APP_URL=https://` + target.Domain + `|`,
			"-e", `s|^DB_DATABASE=.*|DB_DATABASE=` + dbName + `|`,
			"-e", `s|^DB_USERNAME=.*|DB_USERNAME=` + dbUser + `|`,
			"-e", `s|^DB_PASSWORD=.*|DB_PASSWORD=` + dbPass + `|`,
			env},
	}
}

// wpSearchReplaceCommands copies wp-cli from the source home into the
// staging home and rewrites URLs inside the staging database. The copied
// phar is used because the staging user cannot read the source home.
func wpSearchReplaceCommands(src, target model.Website) [][]string {
	srcPhar := "/home/" + src.WebUser + "/.wp-cli/wp-cli.phar"
	dstPhar := "/home/" + target.WebUser + "/.wp-cli/wp-cli.phar"
	php := "/usr/bin/php" + target.PHPVersion
	wp := func(args ...string) []string {
		return append([]string{
			"-u", target.WebUser, "--", php, dstPhar, "--path=" + target.DocumentRoot,
		}, args...)
	}
	return [][]string{
		{"mkdir", "-p", "/home/" + target.WebUser + "/.wp-cli"},
		{"cp", srcPhar, dstPhar},
		{"chown", target.WebUser + ":" + target.WebUser, dstPhar},
		wp("search-replace", "http://"+src.Domain, "http://"+target.Domain, "--report-changed-only"),
		wp("search-replace", "https://"+src.Domain, "https://"+target.Domain, "--report-changed-only"),
		wp("cache", "flush"),
	}
}

var nonIdentifier = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeIdentifier(domain string) string {
	slug := strings.SplitN(domain, ".", 2)[0]
	slug = nonIdentifier.ReplaceAllString(slug, "_")
	return strings.Trim(slug, "_")
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// randomToken returns n alphanumeric characters.
func randomToken(n int) (string, error) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[idx.Int64()]
	}
	return string(out), nil
}
