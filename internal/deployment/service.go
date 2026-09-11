package deployment

import (
	"crypto/subtle"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Service manages git-based deployments for websites.
type Service struct {
	db    *sql.DB
	exec  executor.CommandExecutor
	audit *audit.Service
	queue chan string
}

// NewService creates a new deployment service with a buffered work queue.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{
		db:    db,
		exec:  exec,
		audit: auditSvc,
		queue: make(chan string, 100),
	}
}

// Start launches a background goroutine that processes queued deployments.
// It stops when ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case deploymentID := <-s.queue:
				s.deploy(ctx, deploymentID)
			}
		}
	}()
}

// WebhookDeploy validates the deploy webhook token and starts a deployment
// using the website's stored git repository and branch.
func (s *Service) WebhookDeploy(ctx context.Context, websiteID, token string) (model.Deployment, error) {
	var repo, branch, secret string
	err := s.db.QueryRowContext(ctx,
		`SELECT git_repo, git_branch, deploy_webhook_secret FROM websites WHERE id = ?`,
		websiteID,
	).Scan(&repo, &branch, &secret)
	if err == sql.ErrNoRows {
		return model.Deployment{}, model.ErrNotFound
	}
	if err != nil {
		return model.Deployment{}, fmt.Errorf("load website git config: %w", err)
	}
	if secret == "" || repo == "" {
		return model.Deployment{}, model.NewValidationError("deploy webhook is not configured for this website")
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		return model.Deployment{}, model.ErrForbidden
	}
	return s.Deploy(ctx, websiteID, repo, branch)
}

// Deploy creates a new deployment record in pending status and enqueues it for
// asynchronous processing.
func (s *Service) Deploy(ctx context.Context, websiteID, repo, branch string) (model.Deployment, error) {
	id := ulid.Make().String()
	now := time.Now().UTC().Format(time.RFC3339)

	d := model.Deployment{
		ID:        id,
		WebsiteID: websiteID,
		Branch:    branch,
		Status:    "pending",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deployments (id, website_id, branch, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, websiteID, branch, "pending", now, now,
	)
	if err != nil {
		return model.Deployment{}, fmt.Errorf("insert deployment: %w", err)
	}

	// Store repo URL in the log field temporarily so the worker can retrieve it.
	_, _ = s.db.ExecContext(ctx,
		`UPDATE deployments SET log = ? WHERE id = ?`,
		"repo="+repo, id,
	)

	s.queue <- id

	return d, nil
}

// deployTimeout bounds the entire deployment run (clone of large repos can
// exceed the executor's short default timeout).
const deployTimeout = 15 * time.Minute

// deploy performs the actual git deployment steps for the given deployment ID.
func (s *Service) deploy(ctx context.Context, deploymentID string) {
	// Load the deployment record. On failure mark the deployment failed
	// directly so it does not stay pending forever with no visible error.
	d, err := s.GetDeployment(ctx, deploymentID)
	if err != nil {
		s.failDeployment(ctx, deploymentID, "load deployment: "+err.Error(), 0)
		return
	}

	// Extract repo URL from log field where we temporarily stored it.
	repo := ""
	if strings.HasPrefix(d.Log, "repo=") {
		repo = strings.TrimPrefix(d.Log, "repo=")
	}

	// Load website information.
	var webUser, docRoot string
	err = s.db.QueryRowContext(ctx,
		`SELECT web_user, document_root FROM websites WHERE id = ?`,
		d.WebsiteID,
	).Scan(&webUser, &docRoot)
	if err != nil {
		s.failDeployment(ctx, deploymentID, "load website: "+err.Error(), 0)
		return
	}

	// Update status to running.
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE deployments SET status = ?, log = '', updated_at = ? WHERE id = ?`,
		"running", now, deploymentID,
	)

	start := time.Now()

	// Bound the whole deployment run; the executor's default timeout is too
	// short for cloning real repositories.
	ctx, cancel := context.WithTimeout(ctx, deployTimeout)
	defer cancel()

	var logBuf strings.Builder

	appendLog := func(step string, res *executor.Result, err error) bool {
		logBuf.WriteString("==> " + step + "\n")
		if res != nil {
			if res.Stdout != "" {
				logBuf.WriteString(res.Stdout)
				if !strings.HasSuffix(res.Stdout, "\n") {
					logBuf.WriteString("\n")
				}
			}
			if res.Stderr != "" {
				logBuf.WriteString(res.Stderr)
				if !strings.HasSuffix(res.Stderr, "\n") {
					logBuf.WriteString("\n")
				}
			}
		}
		if err != nil {
			logBuf.WriteString("ERROR: " + err.Error() + "\n")
			return false
		}
		if res != nil && res.ExitCode != 0 {
			logBuf.WriteString(fmt.Sprintf("exit code %d\n", res.ExitCode))
			return false
		}
		return true
	}

	// Check for deploy key and set GIT_SSH_COMMAND if exists.
	homeDir := "/home/" + webUser
	deployKeyPath := homeDir + "/.ssh/deploy_key"
	gitSSHCmd := ""
	res, err := s.exec.Run(ctx, "test", "-f", deployKeyPath)
	if err == nil && res.ExitCode == 0 {
		gitSSHCmd = fmt.Sprintf("GIT_SSH_COMMAND='ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' ", deployKeyPath)
		appendLog("deploy key found", nil, nil)
	}

	// The nginx root (document root) may be nested inside the project root:
	// Laravel sites live in <home>/app with document root <home>/app/public.
	// Git and build steps must operate on the project root.
	projectRoot := docRoot
	if docRoot == homeDir+"/app/public" {
		projectRoot = homeDir + "/app"
	}

	// Step 1: Check if .git dir exists in the project root.
	res, err = s.exec.Run(ctx, "test", "-d", projectRoot+"/.git")
	gitExists := err == nil && res.ExitCode == 0

	// Step 2/3: Clone or pull (run as web_user via sudo -u).
	if gitExists {
		shellCmd := fmt.Sprintf("cd %s && %sgit pull origin %s", projectRoot, gitSSHCmd, d.Branch)
		res, err = s.exec.RunSudo(ctx, "su", "-s", "/bin/bash", "-c", shellCmd, webUser)
		if !appendLog("git pull", res, err) {
			s.failDeployment(ctx, deploymentID, logBuf.String(), int(time.Since(start).Milliseconds()))
			return
		}
	} else {
		shellCmd := fmt.Sprintf("%sgit clone %s %s", gitSSHCmd, repo, projectRoot)
		res, err = s.exec.RunSudo(ctx, "su", "-s", "/bin/bash", "-c", shellCmd, webUser)
		if !appendLog("git clone", res, err) {
			s.failDeployment(ctx, deploymentID, logBuf.String(), int(time.Since(start).Milliseconds()))
			return
		}
		shellCmd = fmt.Sprintf("cd %s && git checkout %s", projectRoot, d.Branch)
		res, err = s.exec.RunSudo(ctx, "su", "-s", "/bin/bash", "-c", shellCmd, webUser)
		if !appendLog("git checkout", res, err) {
			s.failDeployment(ctx, deploymentID, logBuf.String(), int(time.Since(start).Milliseconds()))
			return
		}
	}

	// Step 4: Get commit hash (run as web_user to avoid git "dubious
	// ownership" errors on the project root).
	revCmd := fmt.Sprintf("cd %s && git rev-parse --short HEAD", projectRoot)
	res, err = s.exec.RunSudo(ctx, "su", "-s", "/bin/bash", "-c", revCmd, webUser)
	commitHash := ""
	if err == nil && res.ExitCode == 0 {
		commitHash = strings.TrimSpace(res.Stdout)
	}
	appendLog("git rev-parse", res, err)

	// Step 5: Check if composer.json exists and run composer install.
	res, err = s.exec.Run(ctx, "test", "-f", projectRoot+"/composer.json")
	if err == nil && res.ExitCode == 0 {
		shellCmd := fmt.Sprintf("cd %s && composer install --no-dev --no-interaction", projectRoot)
		res, err = s.exec.RunSudo(ctx, "su", "-s", "/bin/bash", "-c", shellCmd, webUser)
		if !appendLog("composer install", res, err) {
			s.failDeployment(ctx, deploymentID, logBuf.String(), int(time.Since(start).Milliseconds()))
			return
		}
	}

	// Step 6: Check if artisan exists and run Laravel commands.
	res, err = s.exec.Run(ctx, "test", "-f", projectRoot+"/artisan")
	if err == nil && res.ExitCode == 0 {
		artisanCmds := []struct {
			label, cmd string
		}{
			{"artisan migrate", "php artisan migrate --force"},
			{"artisan config:cache", "php artisan config:cache"},
			{"artisan route:cache", "php artisan route:cache"},
			{"artisan view:cache", "php artisan view:cache"},
		}
		for _, ac := range artisanCmds {
			shellCmd := fmt.Sprintf("cd %s && %s", projectRoot, ac.cmd)
			res, err = s.exec.RunSudo(ctx, "su", "-s", "/bin/bash", "-c", shellCmd, webUser)
			if !appendLog(ac.label, res, err) {
				s.failDeployment(ctx, deploymentID, logBuf.String(), int(time.Since(start).Milliseconds()))
				return
			}
		}
	}

	// Step 7: chown the project root (covers a nested document root) and
	// the document root itself.
	res, err = s.exec.RunSudo(ctx, "chown", "-R", webUser+":"+webUser, projectRoot)
	if !appendLog("chown", res, err) {
		s.failDeployment(ctx, deploymentID, logBuf.String(), int(time.Since(start).Milliseconds()))
		return
	}
	if docRoot != projectRoot {
		res, err = s.exec.RunSudo(ctx, "chown", "-R", webUser+":"+webUser, docRoot)
		if !appendLog("chown document root", res, err) {
			s.failDeployment(ctx, deploymentID, logBuf.String(), int(time.Since(start).Milliseconds()))
			return
		}
	}

	// Success — update status.
	duration := int(time.Since(start).Milliseconds())
	now = time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE deployments SET status = ?, commit_hash = ?, duration_ms = ?, log = ?, updated_at = ? WHERE id = ?`,
		"success", commitHash, duration, logBuf.String(), now, deploymentID,
	)
}

// failDeployment marks a deployment as failed with the collected log output.
func (s *Service) failDeployment(ctx context.Context, id, log string, durationMs int) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE deployments SET status = ?, duration_ms = ?, log = ?, updated_at = ? WHERE id = ?`,
		"failed", durationMs, log, now, id,
	)
}

// GetDeployment returns a single deployment by ID.
func (s *Service) GetDeployment(ctx context.Context, id string) (model.Deployment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, website_id, commit_hash, branch, status, duration_ms, log, created_at, updated_at
		 FROM deployments WHERE id = ?`, id,
	)
	d, err := scanDeployment(row)
	if err == sql.ErrNoRows {
		return model.Deployment{}, model.ErrNotFound
	}
	if err != nil {
		return model.Deployment{}, fmt.Errorf("get deployment: %w", err)
	}
	return d, nil
}

// ListByWebsite returns all deployments for a website, newest first.
func (s *Service) ListByWebsite(ctx context.Context, websiteID string) ([]model.Deployment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, commit_hash, branch, status, duration_ms, log, created_at, updated_at
		 FROM deployments WHERE website_id = ? ORDER BY created_at DESC`, websiteID,
	)
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	defer rows.Close()

	var deployments []model.Deployment
	for rows.Next() {
		var d model.Deployment
		var commitHash, log sql.NullString
		var durationMs sql.NullInt64
		var createdStr, updatedStr string

		if err := rows.Scan(
			&d.ID, &d.WebsiteID, &commitHash, &d.Branch, &d.Status,
			&durationMs, &log, &createdStr, &updatedStr,
		); err != nil {
			return nil, fmt.Errorf("scan deployment: %w", err)
		}

		d.CommitHash = commitHash.String
		d.Log = log.String
		d.DurationMs = int(durationMs.Int64)
		d.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		d.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

		deployments = append(deployments, d)
	}

	return deployments, rows.Err()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanDeployment scans a deployment row handling nullable columns.
func scanDeployment(s scanner) (model.Deployment, error) {
	var d model.Deployment
	var commitHash, log sql.NullString
	var durationMs sql.NullInt64
	var createdStr, updatedStr string

	err := s.Scan(
		&d.ID, &d.WebsiteID, &commitHash, &d.Branch, &d.Status,
		&durationMs, &log, &createdStr, &updatedStr,
	)
	if err != nil {
		return model.Deployment{}, err
	}

	d.CommitHash = commitHash.String
	d.Log = log.String
	d.DurationMs = int(durationMs.Int64)
	d.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return d, nil
}
