package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/auth"
	"github.com/mohammadirham37/jenderal_panel/internal/dbdump"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

// ScheduleRequest holds the parameters for creating or updating a backup schedule.
type ScheduleRequest struct {
	Type          string `json:"type"`
	Target        string `json:"target"`
	Storage       string `json:"storage"`
	Schedule      string `json:"schedule"`
	RetentionDays int    `json:"retention_days"`
	RetentionKeep int    `json:"retention_keep"`
}

// Caller identifies who requested a backup operation. SystemCaller runs as
// the panel itself (scheduled backups and prunes).
type Caller struct {
	UserID string
	Admin  bool
}

var SystemCaller = Caller{Admin: true}

// Service manages backup CRUD operations and execution.
type Service struct {
	db       *sql.DB
	exec     executor.CommandExecutor
	audit    *audit.Service
	localDir string
	tasks    *taskrunner.Runner
}

// NewService creates a new backup Service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service, localDir string) *Service {
	if localDir == "" {
		localDir = "/var/lib/jenderal/backups/"
	}
	return &Service{db: db, exec: exec, audit: auditSvc, localDir: localDir}
}

// SetTaskRunner attaches the panel task runner so backups run as visible
// tasks. Without it backups fall back to a bare goroutine (tests only).
func (s *Service) SetTaskRunner(tr *taskrunner.Runner) {
	s.tasks = tr
}

// backup kinds.
const (
	KindManual    = "manual"
	KindScheduled = "scheduled"
	KindSafety    = "safety"
)

// CreateBackup inserts a pending backup record owned by the caller and runs
// the backup as a visible background task.
func (s *Service) CreateBackup(ctx context.Context, caller Caller, backupType, target string) (model.Backup, error) {
	return s.createBackup(ctx, caller, KindManual, backupType, target)
}

// CreateScheduledBackup records and runs a backup on behalf of a schedule.
func (s *Service) CreateScheduledBackup(ctx context.Context, backupType, target string) (model.Backup, error) {
	return s.createBackup(ctx, SystemCaller, KindScheduled, backupType, target)
}

func (s *Service) createBackup(ctx context.Context, caller Caller, kind, backupType, target string) (model.Backup, error) {
	if backupType == "" {
		return model.Backup{}, model.NewValidationError("type is required")
	}
	switch backupType {
	case "website", "database", "config", "full":
	default:
		return model.Backup{}, model.NewValidationError("type must be website, database, config, or full")
	}

	if err := s.authorizeTarget(ctx, caller, backupType, target); err != nil {
		return model.Backup{}, err
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
		Status:    "pending",
		Kind:      kind,
		CreatedBy: caller.UserID,
		CreatedAt: now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO backups (id, type, target, storage, path, size_bytes, status, error_msg, kind, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Type, nullableString(b.Target), b.Storage, b.Path, 0, b.Status,
		nullableString(b.ErrorMsg), b.Kind, b.CreatedBy, nowStr,
	)
	if err != nil {
		return model.Backup{}, fmt.Errorf("insert backup: %w", err)
	}

	if s.tasks != nil {
		name := strings.Title(backupType) + " backup"
		if target != "" {
			name += " — " + target
		}
		b.TaskID = s.tasks.RunFuncWithOptions(taskrunner.Options{Name: name, Module: "backup", Timeout: 2 * time.Hour},
			func(taskCtx context.Context, write func(string)) error {
				return s.executeBackup(taskCtx, b, write)
			})
		_, _ = s.db.ExecContext(ctx, `UPDATE backups SET task_id = ? WHERE id = ?`, b.TaskID, b.ID)
	} else {
		go func() {
			_ = s.executeBackup(context.Background(), b, func(string) {})
		}()
	}

	return b, nil
}

// authorizeTarget verifies the target exists and, for non-admin callers, is
// owned by them. Config/full backups are admin-only.
func (s *Service) authorizeTarget(ctx context.Context, caller Caller, backupType, target string) error {
	switch backupType {
	case "config", "full":
		if !caller.Admin {
			return model.ErrForbidden
		}
		return nil
	case "website":
		if target == "" {
			return model.NewValidationError("target (domain) is required for website backup")
		}
		var createdBy string
		err := s.db.QueryRowContext(ctx,
			`SELECT created_by FROM websites WHERE domain = ?`, target).Scan(&createdBy)
		if err == sql.ErrNoRows {
			return model.NewValidationError("website not found: " + target)
		}
		if err != nil {
			return fmt.Errorf("check website: %w", err)
		}
		if !auth.CanManageResource(caller.Admin, caller.UserID, createdBy) {
			return model.ErrForbidden
		}
		return nil
	case "database":
		if target == "" {
			return model.NewValidationError("target (database name) is required for database backup")
		}
		var createdBy, engine string
		err := s.db.QueryRowContext(ctx,
			`SELECT created_by, engine FROM managed_databases WHERE name = ?`, target).Scan(&createdBy, &engine)
		if err == sql.ErrNoRows {
			return model.NewValidationError("database not found: " + target)
		}
		if err != nil {
			return fmt.Errorf("check database: %w", err)
		}
		if engine == "redis" {
			return model.NewValidationError("redis databases cannot be backed up")
		}
		if !auth.CanManageResource(caller.Admin, caller.UserID, createdBy) {
			return model.ErrForbidden
		}
		return nil
	default:
		return model.NewValidationError("type must be website, database, config, or full")
	}
}

// executeBackup runs the backup commands and updates the database record.
func (s *Service) executeBackup(ctx context.Context, b model.Backup, write func(string)) error {
	write("Starting " + b.Type + " backup…")
	s.updateStatus(ctx, b.ID, "running", "")
	s.setTimestamp(ctx, b.ID, "started_at")

	dir := filepath.Dir(b.Path)
	if _, err := s.exec.RunSudo(ctx, "mkdir", "-p", dir); err != nil {
		err = fmt.Errorf("create directory: %w", err)
		s.failBackup(ctx, b, err)
		return err
	}

	var err error
	switch b.Type {
	case "website":
		err = s.backupWebsite(ctx, b)
	case "database":
		err = s.backupDatabase(ctx, b, write)
	case "config":
		err = s.backupConfig(ctx, b)
	case "full":
		err = s.backupFull(ctx, b, write)
	}

	if err != nil {
		write("Backup failed: " + err.Error())
		s.failBackup(ctx, b, err)
		return err
	}

	sizeBytes := s.getFileSize(ctx, b.Path)
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE backups SET status = ?, size_bytes = ?, path = ?, finished_at = ? WHERE id = ?`,
		"completed", sizeBytes, b.Path, now, b.ID,
	)
	write(fmt.Sprintf("Backup completed (%d bytes).", sizeBytes))

	// Off-site copy: best effort — a failed upload never fails the backup,
	// the local file is the primary copy.
	if b.Kind != KindSafety {
		if cfg, cfgErr := s.GetRemoteConfig(ctx); cfgErr == nil && cfg.enabled() {
			write("Uploading off-site copy…")
			remotePath, upErr := s.UploadToRemote(ctx, b, cfg)
			if upErr != nil {
				write("Off-site upload failed: " + upErr.Error())
			} else {
				_, _ = s.db.ExecContext(ctx,
					`UPDATE backups SET remote_path = ? WHERE id = ?`, remotePath, b.ID)
				write("Off-site copy uploaded: " + remotePath)
			}
		}
	}

	if b.Kind == KindScheduled {
		s.setScheduleLastRunStatus(ctx, b.Type, b.Target, "success")
	}
	return nil
}

func (s *Service) failBackup(ctx context.Context, b model.Backup, backupErr error) {
	s.updateStatus(ctx, b.ID, "failed", backupErr.Error())
	s.setTimestamp(ctx, b.ID, "finished_at")
	if b.Kind == KindScheduled {
		s.setScheduleLastRunStatus(ctx, b.Type, b.Target, "failed")
	}
}

func (s *Service) setTimestamp(ctx context.Context, id, column string) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE backups SET `+column+` = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
}

func (s *Service) backupWebsite(ctx context.Context, b model.Backup) error {
	// Determine the web user directory from the target (domain).
	webUser := b.Target
	if webUser == "" {
		return fmt.Errorf("target (domain) is required for website backup")
	}

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

func (s *Service) backupDatabase(ctx context.Context, b model.Backup, write func(string)) error {
	if b.Target == "" {
		return fmt.Errorf("target (database name) is required for database backup")
	}

	var engine string
	err := s.db.QueryRowContext(ctx,
		`SELECT engine FROM managed_databases WHERE name = ? LIMIT 1`, b.Target,
	).Scan(&engine)
	if err != nil {
		engine = "mysql" // default to mysql
	}

	// Direct argv with a parameterised redirect: the target and path travel
	// as positional shell parameters and are never interpolated.
	bin, args, err := dbdump.DumpToFileCommand(engine, b.Target, b.Path)
	if err != nil {
		return err
	}
	write("Running " + bin + "…")
	result, err := s.exec.RunSudo(ctx, bin, args...)
	if err != nil {
		return fmt.Errorf("database dump: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("database dump failed (exit %d): %s", result.ExitCode, result.Stderr)
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

// manifestComponent describes one file inside a full backup archive.
type manifestComponent struct {
	Kind   string `json:"kind"` // websites | databases | config
	Path   string `json:"path"` // relative to the staging directory
	Target string `json:"target,omitempty"`
}

type backupManifest struct {
	GeneratedAt string              `json:"generated_at"`
	Components  []manifestComponent `json:"components"`
}

func (s *Service) backupFull(ctx context.Context, b model.Backup, write func(string)) error {
	staging := b.Path + ".staging"
	defer func() {
		_, _ = s.exec.RunSudo(ctx, "rm", "-rf", staging)
	}()

	if _, err := s.exec.RunSudo(ctx, "mkdir", "-p", staging); err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}

	var manifest backupManifest
	manifest.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	configPath := filepath.Join(staging, "config.tar.gz")
	cb := model.Backup{Path: configPath}
	write("Backing up configuration…")
	if err := s.backupConfig(ctx, cb); err != nil {
		return fmt.Errorf("full backup config: %w", err)
	}
	manifest.Components = append(manifest.Components,
		manifestComponent{Kind: "config", Path: "config.tar.gz"})

	if b.Target != "" {
		sitePath := filepath.Join(staging, "website.tar.gz")
		wb := model.Backup{Path: sitePath, Target: b.Target}
		write("Backing up website " + b.Target + "…")
		if err := s.backupWebsite(ctx, wb); err != nil {
			return fmt.Errorf("full backup website: %w", err)
		}
		manifest.Components = append(manifest.Components,
			manifestComponent{Kind: "websites", Path: "website.tar.gz", Target: b.Target})
	}

	// All managed relational databases.
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, engine FROM managed_databases WHERE engine IN ('mysql', 'postgresql')`)
	if err != nil {
		return fmt.Errorf("list databases: %w", err)
	}
	type dbRef struct{ name, engine string }
	var databases []dbRef
	for rows.Next() {
		var r dbRef
		if err := rows.Scan(&r.name, &r.engine); err == nil {
			databases = append(databases, r)
		}
	}
	rows.Close()

	for _, r := range databases {
		safe := strings.NewReplacer("/", "_", " ", "_").Replace(r.name)
		dumpPath := filepath.Join(staging, "database_"+safe+".sql")
		write("Backing up database " + r.name + "…")
		bin, args, err := dbdump.DumpToFileCommand(r.engine, r.name, dumpPath)
		if err != nil {
			return err
		}
		result, err := s.exec.RunSudo(ctx, bin, args...)
		if err != nil {
			return fmt.Errorf("dump database %s: %w", r.name, err)
		}
		if result.ExitCode != 0 {
			return fmt.Errorf("dump database %s failed (exit %d): %s", r.name, result.ExitCode, result.Stderr)
		}
		manifest.Components = append(manifest.Components,
			manifestComponent{Kind: "databases", Path: filepath.Base(dumpPath), Target: r.name})
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if _, err := s.exec.RunSudoWithInput(ctx, string(manifestBytes)+"\n",
		"tee", filepath.Join(staging, "manifest.json")); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	write("Packing full archive…")
	result, err := s.exec.RunSudo(ctx, "tar", "-czf", b.Path, "-C", staging, ".")
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

// --- Restore ---

// RequestRestore validates the restore request and runs it as a background
// task. Website and database restores automatically create a safety backup
// of the current state first.
func (s *Service) RequestRestore(ctx context.Context, caller Caller, id, component string) (string, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if b.Status != "completed" {
		return "", model.NewValidationError("can only restore completed backups")
	}
	if !auth.CanManageResource(caller.Admin, caller.UserID, b.CreatedBy) {
		return "", model.ErrForbidden
	}

	component = strings.TrimSpace(strings.ToLower(component))
	switch b.Type {
	case "website":
		component = "websites"
	case "database":
		component = "databases"
	case "config":
		component = "config"
	case "full":
		switch component {
		case "websites", "databases", "config", "":
		default:
			return "", model.NewValidationError("component must be websites, databases, config, or empty for all")
		}
	default:
		return "", model.NewValidationError("unknown backup type: " + b.Type)
	}

	if s.tasks == nil {
		return "", fmt.Errorf("task runner not available")
	}

	taskID := s.tasks.RunFuncWithOptions(taskrunner.Options{
		Name:    "Restore backup — " + b.Target,
		Module:  "backup",
		Timeout: 2 * time.Hour,
	}, func(taskCtx context.Context, write func(string)) error {
		return s.restoreBackup(taskCtx, caller, b, component, write)
	})
	return taskID, nil
}

func (s *Service) restoreBackup(ctx context.Context, caller Caller, b model.Backup, component string, write func(string)) error {
	// Safety first: snapshot the current state so a bad restore can be
	// rolled back. Safety backups use a short 3-day retention.
	if b.Type == "website" || b.Type == "database" || b.Type == "full" {
		write("Creating a safety backup of the current state…")
		safety, err := s.runSafetyBackup(ctx, b.Type, b.Target)
		if err != nil {
			return fmt.Errorf("safety backup: %w", err)
		}
		write("Safety backup saved: " + filepath.Base(safety.Path))
	}

	switch b.Type {
	case "website":
		return s.restoreWebsite(ctx, b)
	case "database":
		return s.restoreDatabase(ctx, b, write)
	case "config":
		return s.restoreConfig(ctx, b)
	case "full":
		return s.restoreFull(ctx, b, component, write)
	default:
		return model.NewValidationError("unknown backup type: " + b.Type)
	}
}

// runSafetyBackup synchronously backs up the same target, tagged kind=safety.
func (s *Service) runSafetyBackup(ctx context.Context, backupType, target string) (model.Backup, error) {
	b, err := s.createBackup(ctx, SystemCaller, KindSafety, backupType, target)
	if err != nil {
		return model.Backup{}, err
	}
	if err := s.executeBackup(ctx, b, func(string) {}); err != nil {
		return model.Backup{}, err
	}
	return s.Get(ctx, b.ID)
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

	// Extraction as root leaves root-owned files behind; give the site back
	// to its web user.
	if _, err := s.exec.RunSudo(ctx, "chown", "-R", webUser+":"+webUser, "/home/"+webUser); err != nil {
		return fmt.Errorf("restore website ownership: %w", err)
	}
	return nil
}

func (s *Service) restoreDatabase(ctx context.Context, b model.Backup, write func(string)) error {
	var engine string
	err := s.db.QueryRowContext(ctx,
		`SELECT engine FROM managed_databases WHERE name = ? LIMIT 1`, b.Target,
	).Scan(&engine)
	if err != nil {
		engine = "mysql"
	}

	// Stream the dump file into the engine through an OS pipe — the dump
	// never buffers in memory. Both paths are internally generated; the
	// database and path travel as positional shell parameters.
	bin, args, err := dbdump.RestorePipelineCommand(engine, b.Target, b.Path)
	if err != nil {
		return err
	}
	write("Restoring into " + b.Target + "…")
	result, err := s.exec.RunSudo(ctx, bin, args...)
	if err != nil {
		return fmt.Errorf("database restore: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("database restore failed (exit %d): %s", result.ExitCode, result.Stderr)
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

// restoreFull restores a full backup archive, limited to the requested
// component ("" restores everything). The archive's manifest.json decides
// what is restored and in which order.
func (s *Service) restoreFull(ctx context.Context, b model.Backup, component string, write func(string)) error {
	staging := b.Path + ".staging"
	if _, err := s.exec.RunSudo(ctx, "rm", "-rf", staging); err != nil {
		return fmt.Errorf("clean staging: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "mkdir", "-p", staging); err != nil {
		return fmt.Errorf("create staging: %w", err)
	}
	defer func() {
		_, _ = s.exec.RunSudo(ctx, "rm", "-rf", staging)
	}()

	result, err := s.exec.RunSudo(ctx, "tar", "-xzf", b.Path, "-C", staging)
	if err != nil {
		return fmt.Errorf("extract full archive: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("extract full archive failed (exit %d): %s", result.ExitCode, result.Stderr)
	}

	manifestResult, err := s.exec.RunSudo(ctx, "cat", filepath.Join(staging, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest backupManifest
	if err := json.Unmarshal([]byte(manifestResult.Stdout), &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	for _, c := range manifest.Components {
		if component != "" && c.Kind != component {
			continue
		}
		componentPath := filepath.Join(staging, c.Path)
		switch c.Kind {
		case "config":
			write("Restoring configuration…")
			cb := model.Backup{Path: componentPath}
			if err := s.restoreConfig(ctx, cb); err != nil {
				return err
			}
		case "websites":
			write("Restoring website " + c.Target + "…")
			wb := model.Backup{Path: componentPath, Target: c.Target}
			if err := s.restoreWebsite(ctx, wb); err != nil {
				return err
			}
		case "databases":
			write("Restoring database " + c.Target + "…")
			db := model.Backup{Path: componentPath, Target: c.Target}
			if err := s.restoreDatabase(ctx, db, write); err != nil {
				return err
			}
		}
	}
	return nil
}

// DownloadBackupStream streams the backup archive to w without buffering it
// whole in memory. Returns the download filename.
func (s *Service) DownloadBackupStream(ctx context.Context, caller Caller, id string, w io.Writer) (string, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !auth.CanManageResource(caller.Admin, caller.UserID, b.CreatedBy) {
		return "", model.ErrForbidden
	}
	if b.Status != "completed" {
		return "", model.NewValidationError("only completed backups can be downloaded")
	}

	filename := filepath.Base(b.Path)
	if filename == "" || filename == "." {
		filename = "backup-" + b.ID
	}
	if _, err := s.exec.RunSudoStream(ctx, w, "cat", b.Path); err != nil {
		return "", fmt.Errorf("stream backup: %w", err)
	}
	return filename, nil
}

// --- Pruning ---

// PruneNow runs the retention pass immediately and returns how many backups
// were removed.
func (s *Service) PruneNow(ctx context.Context) (int, error) {
	schedules, err := s.ListSchedules(ctx, SystemCaller)
	if err != nil {
		return 0, err
	}
	return pruneExpired(ctx, s, schedules, time.Now().UTC()), nil
}

// DeleteBackup removes a backup file and its database record.
func (s *Service) DeleteBackup(ctx context.Context, id string) error {
	b, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

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
	return s.listWhere(ctx, "")
}

// ListByOwner returns the backups created by the given panel user.
func (s *Service) ListByOwner(ctx context.Context, userID string) ([]model.Backup, error) {
	return s.listWhere(ctx, " WHERE created_by = ?", userID)
}

func (s *Service) listWhere(ctx context.Context, where string, args ...any) ([]model.Backup, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, target, storage, path, size_bytes, status, error_msg,
		        kind, created_by, task_id, started_at, finished_at, remote_path, created_at
		 FROM backups`+where+` ORDER BY created_at DESC`, args...)
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
		`SELECT id, type, target, storage, path, size_bytes, status, error_msg,
		        kind, created_by, task_id, started_at, finished_at, remote_path, created_at
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

// BackupStats summarizes the backup directory for the page header.
type BackupStats struct {
	Count      int64 `json:"count"`
	TotalBytes int64 `json:"total_bytes"`
	DiskFree   int64 `json:"disk_free"`
}

// Stats returns counts, total size, and free disk space of the backup store.
func (s *Service) Stats(ctx context.Context) (BackupStats, error) {
	var stats BackupStats
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM backups WHERE status = 'completed'`,
	).Scan(&stats.Count, &stats.TotalBytes)
	if err != nil {
		return stats, fmt.Errorf("backup stats: %w", err)
	}

	result, err := s.exec.RunSudo(ctx, "df", "-B1", "--output=avail", s.localDir)
	if err == nil && result.ExitCode == 0 {
		lines := strings.Fields(strings.TrimSpace(result.Stdout))
		if len(lines) >= 2 {
			stats.DiskFree, _ = strconv.ParseInt(lines[len(lines)-1], 10, 64)
		}
	}
	return stats, nil
}

// --- Schedule operations ---

// CreateSchedule inserts a new backup schedule.
func (s *Service) CreateSchedule(ctx context.Context, caller Caller, req ScheduleRequest) (model.BackupSchedule, error) {
	if err := s.authorizeTarget(ctx, caller, req.Type, req.Target); err != nil {
		return model.BackupSchedule{}, err
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
	retentionKeep := req.RetentionKeep
	if retentionKeep < 0 {
		retentionKeep = 0
	}

	sched := model.BackupSchedule{
		ID:            id,
		Type:          req.Type,
		Target:        req.Target,
		Storage:       storage,
		Schedule:      req.Schedule,
		RetentionDays: retentionDays,
		RetentionKeep: retentionKeep,
		Enabled:       true,
		CreatedBy:     caller.UserID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO backup_schedules (id, type, target, storage, schedule, retention_days, retention_keep, enabled, last_run, last_run_status, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sched.ID, sched.Type, nullableString(sched.Target), sched.Storage,
		sched.Schedule, sched.RetentionDays, sched.RetentionKeep, boolToInt(sched.Enabled),
		nullableString(""), "", sched.CreatedBy, nowStr, nowStr,
	)
	if err != nil {
		return model.BackupSchedule{}, fmt.Errorf("insert backup schedule: %w", err)
	}

	return sched, nil
}

// UpdateSchedule modifies an existing backup schedule.
func (s *Service) UpdateSchedule(ctx context.Context, caller Caller, id string, req ScheduleRequest) error {
	sched, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if !auth.CanManageResource(caller.Admin, caller.UserID, sched.CreatedBy) {
		return model.ErrForbidden
	}
	if err := s.authorizeTarget(ctx, caller, req.Type, req.Target); err != nil {
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
	retentionKeep := req.RetentionKeep
	if retentionKeep < 0 {
		retentionKeep = 0
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE backup_schedules SET type = ?, target = ?, storage = ?, schedule = ?, retention_days = ?, retention_keep = ?, updated_at = ? WHERE id = ?`,
		req.Type, nullableString(req.Target), storage, req.Schedule, retentionDays, retentionKeep, now, id,
	)
	if err != nil {
		return fmt.Errorf("update backup schedule: %w", err)
	}
	return nil
}

// DeleteSchedule removes a backup schedule owned by the caller.
func (s *Service) DeleteSchedule(ctx context.Context, caller Caller, id string) error {
	sched, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if !auth.CanManageResource(caller.Admin, caller.UserID, sched.CreatedBy) {
		return model.ErrForbidden
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM backup_schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete backup schedule: %w", err)
	}
	return nil
}

// ListSchedules returns schedules; non-admin callers see only their own.
func (s *Service) ListSchedules(ctx context.Context, caller Caller) ([]model.BackupSchedule, error) {
	if caller.Admin {
		return s.listSchedulesWhere(ctx, "")
	}
	return s.listSchedulesWhere(ctx, " WHERE created_by = ?", caller.UserID)
}

func (s *Service) listSchedulesWhere(ctx context.Context, where string, args ...any) ([]model.BackupSchedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, target, storage, schedule, retention_days, retention_keep, enabled, last_run, last_run_status, created_by, created_at, updated_at
		 FROM backup_schedules`+where+` ORDER BY created_at DESC`, args...)
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
		`SELECT id, type, target, storage, schedule, retention_days, retention_keep, enabled, last_run, last_run_status, created_by, created_at, updated_at
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
func (s *Service) EnableSchedule(ctx context.Context, caller Caller, id string) error {
	return s.setScheduleEnabled(ctx, caller, id, true)
}

// DisableSchedule disables a backup schedule.
func (s *Service) DisableSchedule(ctx context.Context, caller Caller, id string) error {
	return s.setScheduleEnabled(ctx, caller, id, false)
}

func (s *Service) setScheduleEnabled(ctx context.Context, caller Caller, id string, enabled bool) error {
	sched, err := s.GetSchedule(ctx, id)
	if err != nil {
		return err
	}
	if !auth.CanManageResource(caller.Admin, caller.UserID, sched.CreatedBy) {
		return model.ErrForbidden
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

// setScheduleLastRunStatus records the outcome of a scheduled backup run.
func (s *Service) setScheduleLastRunStatus(ctx context.Context, backupType, target, status string) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE backup_schedules SET last_run = ?, last_run_status = ? WHERE type = ? AND (target = ? OR (target IS NULL AND ? = ''))`,
		now, status, backupType, target, target,
	)
}

// --- Helpers ---

// scanBackup scans a backup row from sql.Rows.
func scanBackup(rows *sql.Rows) (model.Backup, error) {
	var b model.Backup
	var target, errorMsg, kind, createdBy, taskID, startedStr, finishedStr, remotePath sql.NullString
	var sizeBytes sql.NullInt64
	var createdStr string

	err := rows.Scan(
		&b.ID, &b.Type, &target, &b.Storage, &b.Path,
		&sizeBytes, &b.Status, &errorMsg,
		&kind, &createdBy, &taskID, &startedStr, &finishedStr, &remotePath, &createdStr,
	)
	if err != nil {
		return model.Backup{}, err
	}
	return finishBackupScan(b, target, errorMsg, sizeBytes, kind, createdBy, taskID, startedStr, finishedStr, remotePath, sql.NullString{String: createdStr, Valid: true}), nil
}

// scanBackupRow scans a backup from sql.Row.
func scanBackupRow(row *sql.Row) (model.Backup, error) {
	var b model.Backup
	var target, errorMsg, kind, createdBy, taskID, startedStr, finishedStr, remotePath sql.NullString
	var sizeBytes sql.NullInt64
	var createdStr string

	err := row.Scan(
		&b.ID, &b.Type, &target, &b.Storage, &b.Path,
		&sizeBytes, &b.Status, &errorMsg,
		&kind, &createdBy, &taskID, &startedStr, &finishedStr, &remotePath, &createdStr,
	)
	if err != nil {
		return model.Backup{}, err
	}
	return finishBackupScan(b, target, errorMsg, sizeBytes, kind, createdBy, taskID, startedStr, finishedStr, remotePath, sql.NullString{String: createdStr, Valid: true}), nil
}

func finishBackupScan(
	b model.Backup,
	target, errorMsg sql.NullString, sizeBytes sql.NullInt64,
	kind, createdBy, taskID, startedStr, finishedStr, remotePath, createdStr sql.NullString,
) model.Backup {
	b.Target = target.String
	b.ErrorMsg = errorMsg.String
	b.SizeBytes = sizeBytes.Int64
	b.Kind = kind.String
	b.CreatedBy = createdBy.String
	b.TaskID = taskID.String
	b.RemotePath = remotePath.String
	if startedStr.Valid && startedStr.String != "" {
		b.StartedAt, _ = time.Parse(time.RFC3339, startedStr.String)
	}
	if finishedStr.Valid && finishedStr.String != "" {
		b.FinishedAt, _ = time.Parse(time.RFC3339, finishedStr.String)
	}
	b.CreatedAt, _ = time.Parse(time.RFC3339, createdStr.String)
	return b
}

// scanSchedule scans a backup schedule from sql.Rows.
func scanSchedule(rows *sql.Rows) (model.BackupSchedule, error) {
	var s model.BackupSchedule
	var target, lastRun, lastRunStatus, createdBy sql.NullString
	var enabled int
	var createdStr, updatedStr string

	err := rows.Scan(
		&s.ID, &s.Type, &target, &s.Storage, &s.Schedule,
		&s.RetentionDays, &s.RetentionKeep, &enabled, &lastRun, &lastRunStatus, &createdBy,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return model.BackupSchedule{}, err
	}
	return finishScheduleScan(s, target, lastRun, lastRunStatus, createdBy, enabled, createdStr, updatedStr), nil
}

// scanScheduleRow scans a backup schedule from sql.Row.
func scanScheduleRow(row *sql.Row) (model.BackupSchedule, error) {
	var s model.BackupSchedule
	var target, lastRun, lastRunStatus, createdBy sql.NullString
	var enabled int
	var createdStr, updatedStr string

	err := row.Scan(
		&s.ID, &s.Type, &target, &s.Storage, &s.Schedule,
		&s.RetentionDays, &s.RetentionKeep, &enabled, &lastRun, &lastRunStatus, &createdBy,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return model.BackupSchedule{}, err
	}
	return finishScheduleScan(s, target, lastRun, lastRunStatus, createdBy, enabled, createdStr, updatedStr), nil
}

func finishScheduleScan(
	s model.BackupSchedule,
	target, lastRun, lastRunStatus, createdBy sql.NullString,
	enabled int, createdStr, updatedStr string,
) model.BackupSchedule {
	s.Target = target.String
	s.Enabled = enabled == 1
	s.LastRunStatus = lastRunStatus.String
	s.CreatedBy = createdBy.String
	if lastRun.Valid {
		s.LastRun, _ = time.Parse(time.RFC3339, lastRun.String)
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return s
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
