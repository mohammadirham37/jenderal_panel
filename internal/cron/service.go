package cron

import (
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

// CronJobRequest holds the parameters for creating or updating a cron job.
type CronJobRequest struct {
	WebsiteID string `json:"website_id"`
	Command   string `json:"command"`
	Schedule  string `json:"schedule"`
}

// Service manages cron job CRUD operations and system crontab synchronisation.
type Service struct {
	db    *sql.DB
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new cron Service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc}
}

// Create inserts a new cron job record and writes it to the system crontab.
func (s *Service) Create(ctx context.Context, req CronJobRequest) (model.CronJob, error) {
	if req.WebsiteID == "" {
		return model.CronJob{}, model.NewValidationError("website_id is required")
	}
	if req.Command == "" {
		return model.CronJob{}, model.NewValidationError("command is required")
	}
	if req.Schedule == "" {
		return model.CronJob{}, model.NewValidationError("schedule is required")
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	id := ulid.Make().String()

	job := model.CronJob{
		ID:        id,
		WebsiteID: req.WebsiteID,
		Command:   req.Command,
		Schedule:  req.Schedule,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, website_id, command, schedule, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.WebsiteID, job.Command, job.Schedule,
		boolToInt(job.Enabled), nowStr, nowStr,
	)
	if err != nil {
		return model.CronJob{}, fmt.Errorf("insert cron job: %w", err)
	}

	if err := s.rebuildCrontab(ctx, req.WebsiteID); err != nil {
		return model.CronJob{}, fmt.Errorf("rebuild crontab: %w", err)
	}

	return job, nil
}

// List returns all cron jobs ordered by created_at DESC.
func (s *Service) List(ctx context.Context) ([]model.CronJob, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, command, schedule, enabled, last_run, last_status, created_at, updated_at
		 FROM cron_jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list cron jobs: %w", err)
	}
	defer rows.Close()

	var jobs []model.CronJob
	for rows.Next() {
		job, err := scanCronJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cron job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// ListByWebsite returns all cron jobs for a specific website.
func (s *Service) ListByWebsite(ctx context.Context, websiteID string) ([]model.CronJob, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM websites WHERE id = ?`, websiteID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("validate website: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, command, schedule, enabled, last_run, last_status, created_at, updated_at
		 FROM cron_jobs WHERE website_id = ? ORDER BY created_at DESC`, websiteID)
	if err != nil {
		return nil, fmt.Errorf("list cron jobs by website: %w", err)
	}
	defer rows.Close()

	var jobs []model.CronJob
	for rows.Next() {
		job, err := scanCronJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cron job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// Get returns a single cron job by ID.
func (s *Service) Get(ctx context.Context, id string) (model.CronJob, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, website_id, command, schedule, enabled, last_run, last_status, created_at, updated_at
		 FROM cron_jobs WHERE id = ?`, id)

	job, err := scanCronJobRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.CronJob{}, model.ErrNotFound
		}
		return model.CronJob{}, fmt.Errorf("get cron job: %w", err)
	}
	return job, nil
}

// Update modifies an existing cron job and rewrites the system crontab.
func (s *Service) Update(ctx context.Context, id string, req CronJobRequest) error {
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	if req.Command != "" {
		job.Command = req.Command
	}
	if req.Schedule != "" {
		job.Schedule = req.Schedule
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE cron_jobs SET command = ?, schedule = ?, updated_at = ? WHERE id = ?`,
		job.Command, job.Schedule, now, id,
	)
	if err != nil {
		return fmt.Errorf("update cron job: %w", err)
	}

	return s.rebuildCrontab(ctx, job.WebsiteID)
}

// Delete removes a cron job and rewrites the system crontab.
func (s *Service) Delete(ctx context.Context, id string) error {
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete cron job: %w", err)
	}

	return s.rebuildCrontab(ctx, job.WebsiteID)
}

// Enable enables a cron job and rewrites the system crontab.
func (s *Service) Enable(ctx context.Context, id string) error {
	return s.setEnabled(ctx, id, true)
}

// Disable disables a cron job and rewrites the system crontab.
func (s *Service) Disable(ctx context.Context, id string) error {
	return s.setEnabled(ctx, id, false)
}

func (s *Service) setEnabled(ctx context.Context, id string, enabled bool) error {
	job, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE cron_jobs SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolToInt(enabled), now, id,
	)
	if err != nil {
		return fmt.Errorf("set cron job enabled: %w", err)
	}

	return s.rebuildCrontab(ctx, job.WebsiteID)
}

// rebuildCrontab collects all enabled cron jobs for a website, resolves the
// web_user, and writes the full crontab via sudo.
func (s *Service) rebuildCrontab(ctx context.Context, websiteID string) error {
	// Resolve web_user for this website.
	var webUser string
	err := s.db.QueryRowContext(ctx,
		`SELECT web_user FROM websites WHERE id = ?`, websiteID,
	).Scan(&webUser)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.NewDomainError("NOT_FOUND", "website not found", nil)
		}
		return fmt.Errorf("query web_user: %w", err)
	}

	// Gather all enabled cron jobs for this website.
	rows, err := s.db.QueryContext(ctx,
		`SELECT schedule, command FROM cron_jobs
		 WHERE website_id = ? AND enabled = 1
		 ORDER BY created_at ASC`, websiteID)
	if err != nil {
		return fmt.Errorf("query enabled cron jobs: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var schedule, command string
		if err := rows.Scan(&schedule, &command); err != nil {
			return fmt.Errorf("scan cron line: %w", err)
		}
		lines = append(lines, schedule+" "+command)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate cron rows: %w", err)
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}

	// Write via: echo "{content}" | sudo crontab -u {web_user} -
	_, err = s.exec.RunSudo(ctx, "bash", "-c",
		fmt.Sprintf("echo %q | crontab -u %s -", content, webUser))
	if err != nil {
		return fmt.Errorf("write crontab for %s: %w", webUser, err)
	}

	return nil
}

// scanCronJob scans a cron job from sql.Rows.
func scanCronJob(rows *sql.Rows) (model.CronJob, error) {
	var j model.CronJob
	var enabled int
	var lastRun, lastStatus sql.NullString
	var createdStr, updatedStr string

	err := rows.Scan(
		&j.ID, &j.WebsiteID, &j.Command, &j.Schedule,
		&enabled, &lastRun, &lastStatus,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return model.CronJob{}, err
	}

	j.Enabled = enabled == 1
	if lastRun.Valid {
		j.LastRun, _ = time.Parse(time.RFC3339, lastRun.String)
	}
	j.LastStatus = lastStatus.String
	j.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	j.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return j, nil
}

// scanCronJobRow scans a cron job from sql.Row.
func scanCronJobRow(row *sql.Row) (model.CronJob, error) {
	var j model.CronJob
	var enabled int
	var lastRun, lastStatus sql.NullString
	var createdStr, updatedStr string

	err := row.Scan(
		&j.ID, &j.WebsiteID, &j.Command, &j.Schedule,
		&enabled, &lastRun, &lastStatus,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return model.CronJob{}, err
	}

	j.Enabled = enabled == 1
	if lastRun.Valid {
		j.LastRun, _ = time.Parse(time.RFC3339, lastRun.String)
	}
	j.LastStatus = lastStatus.String
	j.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	j.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return j, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
