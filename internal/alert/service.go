package alert

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Service manages alert rules and alert history.
type Service struct {
	db    *sql.DB
	audit *audit.Service
}

// NewService creates a new alert Service.
func NewService(db *sql.DB, auditSvc *audit.Service) *Service {
	return &Service{db: db, audit: auditSvc}
}

// CreateRule inserts a new alert rule.
func (s *Service) CreateRule(ctx context.Context, rule model.AlertRule) (model.AlertRule, error) {
	if rule.Metric == "" {
		return model.AlertRule{}, model.NewValidationError("metric is required")
	}
	if rule.Operator == "" {
		return model.AlertRule{}, model.NewValidationError("operator is required")
	}
	switch rule.Operator {
	case "gt", "lt", "eq":
	default:
		return model.AlertRule{}, model.NewValidationError("operator must be gt, lt, or eq")
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	rule.ID = ulid.Make().String()
	rule.CreatedAt = now
	rule.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_rules (id, metric, operator, threshold, duration_s, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.Metric, rule.Operator, rule.Threshold, rule.DurationS, boolToInt(rule.Enabled), nowStr, nowStr,
	)
	if err != nil {
		return model.AlertRule{}, fmt.Errorf("insert alert rule: %w", err)
	}

	return rule, nil
}

// UpdateRule updates an existing alert rule.
func (s *Service) UpdateRule(ctx context.Context, id string, rule model.AlertRule) error {
	if rule.Metric == "" {
		return model.NewValidationError("metric is required")
	}
	if rule.Operator == "" {
		return model.NewValidationError("operator is required")
	}
	switch rule.Operator {
	case "gt", "lt", "eq":
	default:
		return model.NewValidationError("operator must be gt, lt, or eq")
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)

	res, err := s.db.ExecContext(ctx,
		`UPDATE alert_rules SET metric=?, operator=?, threshold=?, duration_s=?, enabled=?, updated_at=?
		 WHERE id=?`,
		rule.Metric, rule.Operator, rule.Threshold, rule.DurationS, boolToInt(rule.Enabled), nowStr, id,
	)
	if err != nil {
		return fmt.Errorf("update alert rule: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// DeleteRule removes an alert rule by ID.
func (s *Service) DeleteRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// ListRules returns all alert rules.
func (s *Service) ListRules(ctx context.Context) ([]model.AlertRule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, metric, operator, threshold, duration_s, enabled, created_at, updated_at
		 FROM alert_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query alert rules: %w", err)
	}
	defer rows.Close()

	var rules []model.AlertRule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// GetRule returns a single alert rule by ID.
func (s *Service) GetRule(ctx context.Context, id string) (model.AlertRule, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, metric, operator, threshold, duration_s, enabled, created_at, updated_at
		 FROM alert_rules WHERE id=?`, id)

	var r model.AlertRule
	var enabled int
	var createdStr, updatedStr string

	err := row.Scan(&r.ID, &r.Metric, &r.Operator, &r.Threshold, &r.DurationS, &enabled, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return model.AlertRule{}, model.ErrNotFound
	}
	if err != nil {
		return model.AlertRule{}, fmt.Errorf("scan alert rule: %w", err)
	}

	r.Enabled = enabled != 0
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return r, nil
}

// LogAlert inserts a new alert event into the history.
func (s *Service) LogAlert(ctx context.Context, ruleID, metric string, value float64, message string) (model.AlertEvent, error) {
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	id := ulid.Make().String()

	ev := model.AlertEvent{
		ID:        id,
		RuleID:    ruleID,
		Metric:    metric,
		Value:     value,
		Message:   message,
		Resolved:  false,
		CreatedAt: now,
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_history (id, rule_id, metric, value, message, resolved, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.RuleID, ev.Metric, ev.Value, ev.Message, 0, nowStr,
	)
	if err != nil {
		return model.AlertEvent{}, fmt.Errorf("insert alert event: %w", err)
	}

	return ev, nil
}

// ResolveAlert marks an alert event as resolved.
func (s *Service) ResolveAlert(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alert_history SET resolved=1 WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("resolve alert: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// ListHistory returns the most recent alert events, limited by limit.
func (s *Service) ListHistory(ctx context.Context, limit int) ([]model.AlertEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, rule_id, metric, value, message, resolved, created_at
		 FROM alert_history ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query alert history: %w", err)
	}
	defer rows.Close()

	var events []model.AlertEvent
	for rows.Next() {
		var e model.AlertEvent
		var resolved int
		var createdStr string

		if err := rows.Scan(&e.ID, &e.RuleID, &e.Metric, &e.Value, &e.Message, &resolved, &createdStr); err != nil {
			return nil, fmt.Errorf("scan alert event: %w", err)
		}
		e.Resolved = resolved != 0
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		events = append(events, e)
	}
	return events, rows.Err()
}

// scanRule scans a single alert rule from a row scanner.
func scanRule(rows *sql.Rows) (model.AlertRule, error) {
	var r model.AlertRule
	var enabled int
	var createdStr, updatedStr string

	if err := rows.Scan(&r.ID, &r.Metric, &r.Operator, &r.Threshold, &r.DurationS, &enabled, &createdStr, &updatedStr); err != nil {
		return model.AlertRule{}, fmt.Errorf("scan alert rule: %w", err)
	}

	r.Enabled = enabled != 0
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return r, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
