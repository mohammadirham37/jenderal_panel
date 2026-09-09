package queue

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

// systemdUnitTemplate is the Go template for a queue worker systemd unit file.
const systemdUnitTemplate = `[Unit]
Description=Jenderal Queue %s

[Service]
User=%s
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// QueueWorkerRequest holds the parameters for creating a queue worker.
type QueueWorkerRequest struct {
	WebsiteID string `json:"website_id"`
	Command   string `json:"command"`
	NumWorkers int    `json:"num_workers"`
}

// Service manages queue worker CRUD operations and systemd unit management.
type Service struct {
	db    *sql.DB
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new queue Service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc}
}

// Create inserts a new queue worker record, generates a systemd unit file,
// and enables+starts the service.
func (s *Service) Create(ctx context.Context, req QueueWorkerRequest) (model.QueueWorker, error) {
	if req.WebsiteID == "" {
		return model.QueueWorker{}, model.NewValidationError("website_id is required")
	}
	if req.Command == "" {
		return model.QueueWorker{}, model.NewValidationError("command is required")
	}
	if req.NumWorkers < 1 {
		req.NumWorkers = 1
	}

	// Resolve web_user and document_root for this website.
	var webUser, documentRoot string
	err := s.db.QueryRowContext(ctx,
		`SELECT web_user, document_root FROM websites WHERE id = ?`, req.WebsiteID,
	).Scan(&webUser, &documentRoot)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.QueueWorker{}, model.NewDomainError("NOT_FOUND", "website not found", nil)
		}
		return model.QueueWorker{}, fmt.Errorf("query website: %w", err)
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	id := ulid.Make().String()

	worker := model.QueueWorker{
		ID:          id,
		WebsiteID:   req.WebsiteID,
		Command:     req.Command,
		NumWorkers:  req.NumWorkers,
		AutoRestart: true,
		Status:      "stopped",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO queue_workers (id, website_id, command, num_workers, auto_restart, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		worker.ID, worker.WebsiteID, worker.Command, worker.NumWorkers,
		boolToInt(worker.AutoRestart), worker.Status, nowStr, nowStr,
	)
	if err != nil {
		return model.QueueWorker{}, fmt.Errorf("insert queue worker: %w", err)
	}

	// Write systemd unit file, daemon-reload, enable, and start.
	unitContent := fmt.Sprintf(systemdUnitTemplate, id, webUser, documentRoot, req.Command)
	unitPath := unitFilePath(id)

	// Write the unit file via sudo.
	_, err = s.exec.RunSudo(ctx, "bash", "-c",
		fmt.Sprintf("cat > %s << 'UNITEOF'\n%sUNITEOF", unitPath, unitContent))
	if err != nil {
		return worker, fmt.Errorf("write unit file: %w", err)
	}

	unitName := unitFileName(id)

	// Reload, enable, start.
	if _, err := s.exec.RunSudo(ctx, "systemctl", "daemon-reload"); err != nil {
		return worker, fmt.Errorf("daemon-reload: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", "enable", unitName); err != nil {
		return worker, fmt.Errorf("enable unit: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", "start", unitName); err != nil {
		return worker, fmt.Errorf("start unit: %w", err)
	}

	// Update status to running after successful start.
	s.updateStatus(ctx, id, "running")
	worker.Status = "running"

	return worker, nil
}

// Get returns a single queue worker by ID.
func (s *Service) Get(ctx context.Context, id string) (model.QueueWorker, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, website_id, command, num_workers, auto_restart, status, created_at, updated_at
		 FROM queue_workers WHERE id = ?`, id)

	worker, err := scanQueueWorkerRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.QueueWorker{}, model.ErrNotFound
		}
		return model.QueueWorker{}, fmt.Errorf("get queue worker: %w", err)
	}
	return worker, nil
}

// List returns all queue workers ordered by created_at DESC.
func (s *Service) List(ctx context.Context) ([]model.QueueWorker, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, command, num_workers, auto_restart, status, created_at, updated_at
		 FROM queue_workers ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list queue workers: %w", err)
	}
	defer rows.Close()

	var workers []model.QueueWorker
	for rows.Next() {
		w, err := scanQueueWorker(rows)
		if err != nil {
			return nil, fmt.Errorf("scan queue worker: %w", err)
		}
		workers = append(workers, w)
	}
	return workers, rows.Err()
}

// ListByWebsite returns all queue workers for a specific website.
func (s *Service) ListByWebsite(ctx context.Context, websiteID string) ([]model.QueueWorker, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM websites WHERE id = ?`, websiteID).Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("validate website: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, website_id, command, num_workers, auto_restart, status, created_at, updated_at
		 FROM queue_workers WHERE website_id = ? ORDER BY created_at DESC`, websiteID)
	if err != nil {
		return nil, fmt.Errorf("list queue workers by website: %w", err)
	}
	defer rows.Close()

	var workers []model.QueueWorker
	for rows.Next() {
		w, err := scanQueueWorker(rows)
		if err != nil {
			return nil, fmt.Errorf("scan queue worker: %w", err)
		}
		workers = append(workers, w)
	}
	return workers, rows.Err()
}

// Start starts a queue worker's systemd service.
func (s *Service) Start(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}

	unitName := unitFileName(id)
	if _, err := s.exec.RunSudo(ctx, "systemctl", "start", unitName); err != nil {
		return fmt.Errorf("start queue worker: %w", err)
	}

	return s.updateStatus(ctx, id, "running")
}

// Stop stops a queue worker's systemd service.
func (s *Service) Stop(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}

	unitName := unitFileName(id)
	if _, err := s.exec.RunSudo(ctx, "systemctl", "stop", unitName); err != nil {
		return fmt.Errorf("stop queue worker: %w", err)
	}

	return s.updateStatus(ctx, id, "stopped")
}

// Restart restarts a queue worker's systemd service.
func (s *Service) Restart(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}

	unitName := unitFileName(id)
	if _, err := s.exec.RunSudo(ctx, "systemctl", "restart", unitName); err != nil {
		return fmt.Errorf("restart queue worker: %w", err)
	}

	return s.updateStatus(ctx, id, "running")
}

// Delete stops and removes a queue worker's systemd service, then deletes the
// DB record.
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}

	unitName := unitFileName(id)
	unitPath := unitFilePath(id)

	// Stop and disable the service (ignore errors if already stopped).
	_, _ = s.exec.RunSudo(ctx, "systemctl", "stop", unitName)
	_, _ = s.exec.RunSudo(ctx, "systemctl", "disable", unitName)
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", unitPath)
	_, _ = s.exec.RunSudo(ctx, "systemctl", "daemon-reload")

	_, err := s.db.ExecContext(ctx, `DELETE FROM queue_workers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete queue worker: %w", err)
	}

	return nil
}

// Status returns the systemctl status output for a queue worker.
func (s *Service) Status(ctx context.Context, id string) (string, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return "", err
	}

	unitName := unitFileName(id)
	result, err := s.exec.RunSudo(ctx, "systemctl", "status", unitName)
	if err != nil {
		return "", fmt.Errorf("systemctl status: %w", err)
	}

	return strings.TrimSpace(result.Stdout), nil
}

func (s *Service) updateStatus(ctx context.Context, id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE queue_workers SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id,
	)
	if err != nil {
		return fmt.Errorf("update queue worker status: %w", err)
	}
	return nil
}

func unitFileName(id string) string {
	return "jenderal-queue-" + id + ".service"
}

func unitFilePath(id string) string {
	return "/etc/systemd/system/" + unitFileName(id)
}

// scanQueueWorker scans a queue worker from sql.Rows.
func scanQueueWorker(rows *sql.Rows) (model.QueueWorker, error) {
	var w model.QueueWorker
	var autoRestart int
	var createdStr, updatedStr string

	err := rows.Scan(
		&w.ID, &w.WebsiteID, &w.Command, &w.NumWorkers,
		&autoRestart, &w.Status,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return model.QueueWorker{}, err
	}

	w.AutoRestart = autoRestart == 1
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return w, nil
}

// scanQueueWorkerRow scans a queue worker from sql.Row.
func scanQueueWorkerRow(row *sql.Row) (model.QueueWorker, error) {
	var w model.QueueWorker
	var autoRestart int
	var createdStr, updatedStr string

	err := row.Scan(
		&w.ID, &w.WebsiteID, &w.Command, &w.NumWorkers,
		&autoRestart, &w.Status,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return model.QueueWorker{}, err
	}

	w.AutoRestart = autoRestart == 1
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

	return w, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
