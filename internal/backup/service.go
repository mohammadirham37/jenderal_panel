package backup

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// ScheduleRequest holds the parameters for creating or updating a backup schedule.
type ScheduleRequest struct {
	Type          string `json:"type"`
	Target        string `json:"target"`
	Storage       string `json:"storage"`
	Schedule      string `json:"schedule"`
	RetentionDays int    `json:"retention_days"`
}

// Service manages backup CRUD operations and execution.
type Service struct {
	db       *sql.DB
	exec     executor.CommandExecutor
	audit    *audit.Service
	localDir string
}

// NewService creates a new backup Service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service, localDir string) *Service {
	if localDir == "" {
		localDir = "/var/lib/jenderal/backups/"
	}
	return &Service{db: db, exec: exec, audit: auditSvc, localDir: localDir}
}

// CreateBackup inserts a pending backup record and runs the backup in a background goroutine.
func (s *Service) CreateBackup(ctx context.Context, backupType, target string) (model.Backup, error) {
	if backupType == "" {
		return model.Backup{}, model.NewValidationError("type is required")
	}
	switch backupType {
	case "website", "database", "config", "full":
	default:
		return model.Backup{}, model.NewValidationError("type must be website, database, config, or full")
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	id := ulid.Make().String()
	genPath := generatePath(s.localDir, backupType, target)

	b := model.Backup{
		ID:        id,
		Type:      backupType,
		Target:    target,
		Storage:   "local",
		Path:      genPath,
		SizeBytes: 0,
		Status:    "pending",
		CreatedAt: now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO backups (id, type, target, storage, path, size_bytes, status, error_msg, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Type, nullableString(b.Target), b.Storage, b.Path, b.SizeBytes, b.Status, nullableString(b.ErrorMsg), nowStr,
	)
	if err != nil {
		return model.Backup{}, fmt.Errorf("insert backup: %w", err)
	}

	// Run the actual backup in the background.
	go s.runBackup(b)

	return b, nil
}

// runBackup executes the backup commands and updates the database record.
func (s *Service) runBackup(b model.Backup) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Update status to running.
	s.updateStatus(ctx, b.ID, "running", "")

	// Ensure target directory exists.
	dir := filepath.Dir(b.Path)
	if _, err := s.exec.RunSudo(ctx, "mkdir", "-p", dir); err != nil {
		s.updateStatus(ctx, b.ID, "failed", fmt.Sprintf("create directory: %v", err))
		return
	}

	var err error
	switch b.Type {
	case "website":
		err = s.backupWebsite(ctx, b)
	case "database":
		err = s.backupDatabase(ctx, b)
	case "config":
		err = s.backupConfig(ctx, b)
	case "full":
		err = s.backupFull(ctx, b)
	}

	if err != nil {
		s.updateStatus(ctx, b.ID, "failed", err.Error())
		return
	}

	// Get file size.
	sizeBytes := s.getFileSize(ctx, b.Path)
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE backups SET status = ?, size_bytes = ?, path = ?, created_at = COALESCE(created_at, ?) WHERE id = ?`,
		"completed", sizeBytes, b.Path, now, b.ID,
	)
}

func (s *Service) backupWebsite(ctx context.Context, b model.Backup) error {
	// Determine the web user directory from the target (domain).
	webUser := b.Target
	if webUser == "" {
		return fmt.Errorf("target (domain) is required for website backup")
	}

	// Look up web_user from the websites table by domain.
	var user string
	err := s.db.QueryRowContext(ctx,
		`SELECT web_user FROM websites WHERE domain = ? LIMIT 1`, b.Target,
	).Scan(&user)
	if err == nil {
		webUser = user
	}

	result, err := s.exec.RunSudo(ctx, "tar", "-czf", b.Path, "-C", "/home/"+webUser, ".")
	if err != nil {
		return fmt.Errorf("tar website: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("tar website failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	return nil
}

func (s *Service) backupDatabase(ctx context.Context, b model.Backup) error {
	if b.Target == "" {
		return fmt.Errorf("target (database name) is required for database backup")
	}

	// Try to detect the engine from managed_databases table.
	var engine string
	err := s.db.QueryRowContext(ctx,
		`SELECT engine FROM managed_databases WHERE name = ? LIMIT 1`, b.Target,
	).Scan(&engine)
	if err != nil {
		engine = "mysql" // default to mysql
	}

	switch engine {
	case "mysql", "mariadb":
		result, err := s.exec.RunSudo(ctx, "bash", "-c",
			fmt.Sprintf("mysqldump %s > %s", b.Target, b.Path))
		if err != nil {
			return fmt.Errorf("mysqldump: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("mysqldump failed (exit %d): %s", result.ExitCode, result.Stderr)
		}
	case "postgresql", "postgres":
		result, err := s.exec.RunSudo(ctx, "bash", "-c",
			fmt.Sprintf("sudo -u postgres pg_dump %s > %s", b.Target, b.Path))
		if err != nil {
			return fmt.Errorf("pg_dump: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("pg_dump failed (exit %d): %s", result.ExitCode, result.Stderr)
		}
	default:
		return fmt.Errorf("unsupported database engine: %s", engine)
	}

	return nil
}

func (s *Service) backupConfig(ctx context.Context, b model.Backup) error {
	result, err := s.exec.RunSudo(ctx, "tar", "-czf", b.Path, "/etc/jenderal", "/etc/nginx")
	if err != nil {
		return fmt.Errorf("tar config: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("tar config failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	return nil
}

func (s *Service) backupFull(ctx context.Context, b model.Backup) error {
	// For a full backup, run website + database + config as individual sub-backups.
	// Website: back up the target if specified.
	if b.Target != "" {
		websitePath := strings.TrimSuffix(b.Path, ".tar.gz") + "_website.tar.gz"
		wb := model.Backup{Path: websitePath, Target: b.Target}
		if err := s.backupWebsite(ctx, wb); err != nil {
			return fmt.Errorf("full backup website: %w", err)
		}
	}

	// Config backup.
	configPath := strings.TrimSuffix(b.Path, ".tar.gz") + "_config.tar.gz"
	cb := model.Backup{Path: configPath}
	if err := s.backupConfig(ctx, cb); err != nil {
		return fmt.Errorf("full backup config: %w", err)
	}

	// Create a combined archive of all generated sub-backups.
	dir := filepath.Dir(b.Path)
	result, err := s.exec.RunSudo(ctx, "tar", "-czf", b.Path, "-C", dir, ".")
	if err != nil {
		return fmt.Errorf("tar full: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("tar full failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	return nil
}

func (s *Service) updateStatus(ctx context.Context, id, status, errorMsg string) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE backups SET status = ?, error_msg = ? WHERE id = ?`,
		status, nullableString(errorMsg), id,
	)
}

func (s *Service) getFileSize(ctx context.Context, path string) int64 {
	result, err := s.exec.RunSudo(ctx, "stat", "--printf=%s", path)
	if err != nil {
		return 0
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(result.Stdout), 10, 64)
	return size
}

// RestoreBackup restores a backup by ID.
func (s *Service) RestoreBackup(ctx context.Context, id string) error {
	b, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if b.Status != "completed" {
		return model.NewValidationError("can only restore completed backups")
	}

	switch b.Type {
	case "website":
		return s.restoreWebsite(ctx, b)
	case "database":
		return s.restoreDatabase(ctx, b)
	case "config":
		return s.restoreConfig(ctx, b)
	case "full":
		return model.NewValidationError("full backups must be restored component by component")
	default:
		return model.NewValidationError("unknown backup type: " + b.Type)
	}
}

func (s *Service) restoreWebsite(ctx context.Context, b model.Backup) error {
	webUser := b.Target
	var user string
	err := s.db.QueryRowContext(ctx,
		`SELECT web_user FROM websites WHERE domain = ? LIMIT 1`, b.Target,
	).Scan(&user)
	if err == nil {
		webUser = user
	}

	result, err := s.exec.RunSudo(ctx, "tar", "-xzf", b.Path, "-C", "/home/"+webUser)
	if err != nil {
		return fmt.Errorf("restore website: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("restore website failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	return nil
}

func (s *Service) restoreDatabase(ctx context.Context, b model.Backup) error {
	var engine string
	err := s.db.QueryRowContext(ctx,
		`SELECT engine FROM managed_databases WHERE name = ? LIMIT 1`, b.Target,
	).Scan(&engine)
	if err != nil {
		engine = "mysql"
	}

	switch engine {
	case "mysql", "mariadb":
		result, err := s.exec.RunSudo(ctx, "bash", "-c",
			fmt.Sprintf("mysql %s < %s", b.Target, b.Path))
		if err != nil {
			return fmt.Errorf("mysql restore: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("mysql restore failed (exit %d): %s", result.ExitCode, result.Stderr)
		}
	case "postgresql", "postgres":
		result, err := s.exec.RunSudo(ctx, "bash", "-c",
			fmt.Sprintf("sudo -u postgres pg_restore -d %s %s", b.Target, b.Path))
		if err != nil {
			return fmt.Errorf("pg_restore: %w", err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("pg_restore failed (exit %d): %s", result.ExitCode, result.Stderr)
		}
	default:
		return fmt.Errorf("unsupported database engine: %s", engine)
	}
	return nil
}

func (s *Service) restoreConfig(ctx context.Context, b model.Backup) error {
	result, err := s.exec.RunSudo(ctx, "tar", "-xzf", b.Path, "-C", "/")
	if err != nil {
		return fmt.Errorf("restore config: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("restore config failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	return nil
}

// DeleteBackup removes a backup file and its database record.
func (s *Service) DeleteBackup(ctx context.Context, id string) error {
	b, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	// Remove the file.
	if b.Path != "" {
		_, _ = s.exec.RunSudo(ctx, "rm", "-f", b.Path)
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM backups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete backup: %w", err)
	}
	return nil
}

// List returns all backups ordered by created_at DESC.
func (s *Service) List(ctx context.Context) ([]model.Backup, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, target, storage, path, size_bytes, status, error_msg, created_at
		 FROM backups ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	defer rows.Close()

	var backups []model.Backup
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, fmt.Errorf("scan backup: %w", err)
		}
		backups = append(backups, b)
	}
	return backups, rows.Err()
}

// Get returns a single backup by ID.
func (s *Service) Get(ctx context.Context, id string) (model.Backup, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, type, target, storage, path, size_bytes, status, error_msg, created_at
		 FROM backups WHERE id = ?`, id)

	b, err := scanBackupRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.Backup{}, model.ErrNotFound
		}
		return model.Backup{}, fmt.Errorf("get backup: %w", err)
	}
	return b, nil
}

// --- Schedule operations ---

// CreateSchedule inserts a new backup schedule.
func (s *Service) CreateSchedule(ctx context.Context, req ScheduleRequest) (model.BackupSchedule, error) {
	if req.Type == "" {
		return model.BackupSchedule{}, model.NewValidationError("type is required")
	}
	if req.Schedule == "" {
		return model.BackupSchedule{}, model.NewValidationError("schedule is required")
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	id := ulid.Make().String()

	storage := req.Storage
	if storage == "" {
		storage = "local"
	}
	retentionDays := req.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 7
	}

	sched := model.BackupSchedule{
		ID:            id,
		Type:          req.Type,
		Target:        req.Target,
		Storage:       storage,
		Schedule:      req.Schedule,
		RetentionDays: retentionDays,
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_schedules (id, type, target, storage, schedule, retention_days, enabled, last_run, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sched.ID, sched.Type, nullableString(sched.Target), sched.Storage,
		sched.Schedule, sched.RetentionDays, boolToInt(sched.Enabled),
		nullableString(""), nowStr, nowStr,
	)
	if err != nil {
		return model.BackupSchedule{}, fmt.Errorf("insert backup schedule: %w", err)
	}

	return sched, nil
}

// UpdateSchedule modifies an existing backup schedule.
func (s *Service) UpdateSchedule(ctx context.Context, id string, req ScheduleRequest) error {
	_, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	storage := req.Storage
	if storage == "" {
		storage = "local"
	}
	retentionDays := req.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 7
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE backup_schedules SET type = ?, target = ?, storage = ?, schedule = ?, retention_days = ?, updated_at = ? WHERE id = ?`,
		req.Type, nullableString(req.Target), storage, req.Schedule, retentionDays, now, id,
	)
	if err != nil {
		return fmt.Errorf("update backup schedule: %w", err)
	}
	return nil
}

// DeleteSchedule removes a backup schedule.
func (s *Service) DeleteSchedule(ctx context.Context, id string) error {
	_, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM backup_schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete backup schedule: %w", err)
	}
	return nil
}

// ListSchedules returns all backup schedules ordered by created_at DESC.
func (s *Service) ListSchedules(ctx context.Context) ([]model.BackupSchedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, target, storage, schedule, retention_days, enabled, last_run, created_at, updated_at
		 FROM backup_schedules ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list backup schedules: %w", err)
	}
	defer rows.Close()

	var schedules []model.BackupSchedule
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan backup schedule: %w", err)
		}
		schedules = append(schedules, sched)
	}
	return schedules, rows.Err()
}

// GetSchedule returns a single backup schedule by ID.
func (s *Service) GetSchedule(ctx context.Context, id string) (model.BackupSchedule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, type, target, storage, schedule, retention_days, enabled, last_run, created_at, updated_at
		 FROM backup_schedules WHERE id = ?`, id)

	sched, err := scanScheduleRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.BackupSchedule{}, model.ErrNotFound
		}
		return model.BackupSchedule{}, fmt.Errorf("get backup schedule: %w", err)
	}
	return sched, nil
}

// EnableSchedule enables a backup schedule.
func (s *Service) EnableSchedule(ctx context.Context, id string) error {
	return s.setScheduleEnabled(ctx, id, true)
}

// DisableSchedule disables a backup schedule.
func (s *Service) DisableSchedule(ctx context.Context, id string) error {
	return s.setScheduleEnabled(ctx, id, false)
}

func (s *Service) setScheduleEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE backup_schedules SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolToInt(enabled), now, id,
	)
	if err != nil {
		return fmt.Errorf("set backup schedule enabled: %w", err)
	}
	return nil
}

// --- Helpers ---

// scanBackup scans a backup row from sql.Rows.
func scanBackup(rows *sql.Rows) (model.Backup, error) {
	var b model.Backup
	var target, errorMsg sql.NullString
	var sizeBytes sql.NullInt64
	var createdStr string

	err := rows.Scan(
		&b.ID, &b.Type, &target, &b.Storage, &b.Path,
		&sizeBytes, &b.Status, &errorMsg, &createdStr,
	)
	if err != nil {
		return model.Backup{}, err
	}

	b.Target = target.String
	b.ErrorMsg = errorMsg.String
	b.SizeBytes = sizeBytes.Int64
	b.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

	return b, nil
}

// scanBackupRow scans a backup from sql.Row.
func scanBackupRow(row *sql.Row) (model.Backup, error) {
	var b model.Backup
	var target, errorMsg sql.NullString
	var sizeBytes sql.NullInt64
	var createdStr string

	err := row.Scan(
		&b.ID, &b.Type, &target, &b.Storage, &b.Path,
		&sizeBytes, &b.Status, &errorMsg, &createdStr,
	)
	if err != nil {
		return model.Backup{}, err
	}

	b.Target = target.String
	b.ErrorMsg = errorMsg.String
	b.SizeBytes = sizeBytes.Int64
	b.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

	return b, nil
}

// scanSchedule scans a backup schedule from sql.Rows.
func scanSchedule(rows *sql.Rows) (model.BackupSchedule, error) {
	var s model.BackupSchedule
	var target, lastRun sql.NullString
	var enabled int
	var createdStr, updatedStr string

	err := rows.Scan(
		&s.ID, &s.Type, &target, &s.Storage, &s.Schedule,
		&s.RetentionDays, &enabled, &lastRun,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return model.BackupSchedule{}, err
	}

	s.Target = target.String
	s.Enabled = enabled == 1
	if lastRun.Valid {
		s.LastRun, _ = time.Parse(time.RFC3339, lastRun.String)
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return s, nil
}

// scanScheduleRow scans a backup schedule from sql.Row.
func scanScheduleRow(row *sql.Row) (model.BackupSchedule, error) {
	var s model.BackupSchedule
	var target, lastRun sql.NullString
	var enabled int
	var createdStr, updatedStr string

	err := row.Scan(
		&s.ID, &s.Type, &target, &s.Storage, &s.Schedule,
		&s.RetentionDays, &enabled, &lastRun,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return model.BackupSchedule{}, err
	}

	s.Target = target.String
	s.Enabled = enabled == 1
	if lastRun.Valid {
		s.LastRun, _ = time.Parse(time.RFC3339, lastRun.String)
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return s, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// generatePath builds a file path for a backup based on type and target.
func generatePath(localDir, backupType, target string) string {
	timestamp := time.Now().UTC().Format("20060102_150405")
	safeName := target
	if safeName == "" {
		safeName = "all"
	}
	// Sanitise the target name for use in file paths.
	safeName = strings.ReplaceAll(safeName, "/", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")

	switch backupType {
	case "website":
		return filepath.Join(localDir, "website", fmt.Sprintf("%s_%s.tar.gz", timestamp, safeName))
	case "database":
		return filepath.Join(localDir, "database", fmt.Sprintf("%s_%s.sql", timestamp, safeName))
	case "config":
		return filepath.Join(localDir, "config", fmt.Sprintf("%s_config.tar.gz", timestamp))
	case "full":
		return filepath.Join(localDir, "full", fmt.Sprintf("%s_%s.tar.gz", timestamp, safeName))
	default:
		return filepath.Join(localDir, backupType, fmt.Sprintf("%s_%s.tar.gz", timestamp, safeName))
	}
}
