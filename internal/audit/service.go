package audit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// LogEntry holds the data for creating an audit log entry.
type LogEntry struct {
	UserID string
	Action string
	Module string
	Target string
	Detail string
	IP     string
}

// ListParams holds pagination and filter parameters.
type ListParams struct {
	Page    int
	PerPage int
	Module  string
}

// Log inserts a new audit log entry.
func (s *Service) Log(ctx context.Context, entry LogEntry) error {
	id := ulid.Make().String()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (id, user_id, action, module, target, detail, ip_address, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		nullableString(entry.UserID),
		entry.Action,
		entry.Module,
		nullableString(entry.Target),
		nullableString(entry.Detail),
		nullableString(entry.IP),
		now,
	)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}

	return nil
}

// List returns paginated audit entries, newest first, with optional module filter.
// Returns the entries, total count, and any error.
func (s *Service) List(ctx context.Context, params ListParams) ([]model.AuditEntry, int, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PerPage < 1 {
		params.PerPage = 10
	}

	// Build count query
	countQuery := `SELECT COUNT(*) FROM audit_logs`
	listQuery := `SELECT id, user_id, action, module, target, detail, ip_address, created_at FROM audit_logs`

	var args []any
	if params.Module != "" {
		countQuery += ` WHERE module = ?`
		listQuery += ` WHERE module = ?`
		args = append(args, params.Module)
	}

	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	listQuery += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	offset := (params.Page - 1) * params.PerPage
	listArgs := append(args, params.PerPage, offset)

	rows, err := s.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var entries []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		var userID, target, detail, ip sql.NullString
		var createdStr string

		if err := rows.Scan(&e.ID, &userID, &e.Action, &e.Module, &target, &detail, &ip, &createdStr); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}

		e.UserID = userID.String
		e.Target = target.String
		e.Detail = detail.String
		e.IPAddress = ip.String
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

		entries = append(entries, e)
	}

	return entries, total, rows.Err()
}

// nullableString returns nil if s is empty, otherwise returns s.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
