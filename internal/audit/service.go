package audit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	// From and To bound created_at (RFC3339 or "2006-01-02"; a date-only To
	// covers the whole day).
	From   string
	To     string
	Search string
}

// normalizeDateBound normalizes a date filter bound. Date-only values become
// the start (00:00:00Z) or the end (23:59:59Z) of that UTC day; RFC3339
// values pass through. Returns ok=false for unparsable input.
func normalizeDateBound(value string, end bool) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.UTC().Format(time.RFC3339), true
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		if end {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t.UTC().Format(time.RFC3339), true
	}
	return "", false
}

// Modules returns the distinct modules present in the audit log.
func (s *Service) Modules(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT module FROM audit_logs ORDER BY module`)
	if err != nil {
		return nil, fmt.Errorf("list audit modules: %w", err)
	}
	defer rows.Close()

	var modules []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	return modules, rows.Err()
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

	// Build filters: module, date bounds, and a text search across the
	// readable columns. The users join resolves display names.
	var conditions []string
	var args []any
	if params.Module != "" {
		conditions = append(conditions, "l.module = ?")
		args = append(args, params.Module)
	}
	if from, ok := normalizeDateBound(params.From, false); ok {
		conditions = append(conditions, "l.created_at >= ?")
		args = append(args, from)
	}
	if to, ok := normalizeDateBound(params.To, true); ok {
		conditions = append(conditions, "l.created_at <= ?")
		args = append(args, to)
	}
	if params.Search != "" {
		like := "%" + params.Search + "%"
		conditions = append(conditions, "(l.action LIKE ? OR l.target LIKE ? OR l.detail LIKE ? OR l.user_id LIKE ? OR u.email LIKE ?)")
		for range [5]struct{}{} {
			args = append(args, like)
		}
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	// Build count query
	countQuery := `SELECT COUNT(*) FROM audit_logs l LEFT JOIN users u ON u.id = l.user_id` + where
	listQuery := `SELECT l.id, l.user_id, COALESCE(u.email, '') AS username, l.action, l.module, l.target, l.detail, l.ip_address, l.created_at
		FROM audit_logs l LEFT JOIN users u ON u.id = l.user_id` + where

	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}

	listQuery += ` ORDER BY l.created_at DESC LIMIT ? OFFSET ?`
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
		var username, target, detail, ip sql.NullString
		var createdStr string

		if err := rows.Scan(&e.ID, &e.UserID, &username, &e.Action, &e.Module, &target, &detail, &ip, &createdStr); err != nil {
			return nil, 0, fmt.Errorf("scan audit log: %w", err)
		}

		e.Username = username.String
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
