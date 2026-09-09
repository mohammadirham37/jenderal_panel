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
	db       *sql.DB
	audit    *audit.Service
	services ServiceStatusProvider
	certs    CertificateProvider
}

// NewService creates a new alert Service.
func NewService(db *sql.DB, auditSvc *audit.Service) *Service {
	return &Service{db: db, audit: auditSvc}
}

// SetTargetProviders configures the sources used to validate target-aware rules.
func (s *Service) SetTargetProviders(services ServiceStatusProvider, certs CertificateProvider) {
	s.services = services
	s.certs = certs
}

// CreateRule inserts a new alert rule.
func (s *Service) CreateRule(ctx context.Context, rule model.AlertRule) (model.AlertRule, error) {
	if err := s.validateRule(ctx, &rule); err != nil {
		return model.AlertRule{}, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	rule.ID = ulid.Make().String()
	rule.CreatedAt = now
	rule.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AlertRule{}, fmt.Errorf("begin alert rule insert: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO alert_rules (id, metric, operator, threshold, duration_s, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.Metric, rule.Operator, rule.Threshold, rule.DurationS, boolToInt(rule.Enabled), nowStr, nowStr,
	)
	if err != nil {
		return model.AlertRule{}, fmt.Errorf("insert alert rule: %w", err)
	}
	if rule.Target != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alert_rule_targets (rule_id, target) VALUES (?, ?)`, rule.ID, rule.Target); err != nil {
			return model.AlertRule{}, fmt.Errorf("insert alert rule target: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return model.AlertRule{}, fmt.Errorf("commit alert rule insert: %w", err)
	}

	return rule, nil
}

// UpdateRule updates an existing alert rule.
func (s *Service) UpdateRule(ctx context.Context, id string, rule model.AlertRule) error {
	if err := s.validateRule(ctx, &rule); err != nil {
		return err
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin alert rule update: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
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
	if rule.Target == "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM alert_rule_targets WHERE rule_id=?`, id); err != nil {
			return fmt.Errorf("delete alert rule target: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `INSERT INTO alert_rule_targets (rule_id, target) VALUES (?, ?)
		ON CONFLICT(rule_id) DO UPDATE SET target=excluded.target`, id, rule.Target); err != nil {
		return fmt.Errorf("update alert rule target: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit alert rule update: %w", err)
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
		`SELECT r.id, r.metric, COALESCE(t.target, ''), r.operator, r.threshold, r.duration_s, r.enabled, r.created_at, r.updated_at
		 FROM alert_rules r LEFT JOIN alert_rule_targets t ON t.rule_id=r.id ORDER BY r.created_at DESC`)
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
		`SELECT r.id, r.metric, COALESCE(t.target, ''), r.operator, r.threshold, r.duration_s, r.enabled, r.created_at, r.updated_at
		 FROM alert_rules r LEFT JOIN alert_rule_targets t ON t.rule_id=r.id WHERE r.id=?`, id)

	var r model.AlertRule
	var enabled int
	var createdStr, updatedStr string

	err := row.Scan(&r.ID, &r.Metric, &r.Target, &r.Operator, &r.Threshold, &r.DurationS, &enabled, &createdStr, &updatedStr)
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

// ResolveAlertsByRule marks every unresolved event for a rule as resolved.
func (s *Service) ResolveAlertsByRule(ctx context.Context, ruleID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE alert_history SET resolved=1 WHERE rule_id=? AND resolved=0`, ruleID)
	if err != nil {
		return 0, fmt.Errorf("resolve alerts by rule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count resolved alerts: %w", err)
	}
	return n, nil
}

// FindUnresolvedByRule returns the newest unresolved event for a rule.
func (s *Service) FindUnresolvedByRule(ctx context.Context, ruleID string) (model.AlertEvent, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, rule_id, metric, value, message, resolved, created_at
		FROM alert_history WHERE rule_id=? AND resolved=0 ORDER BY created_at DESC LIMIT 1`, ruleID)
	var event model.AlertEvent
	var resolved int
	var created string
	if err := row.Scan(&event.ID, &event.RuleID, &event.Metric, &event.Value, &event.Message, &resolved, &created); err != nil {
		if err == sql.ErrNoRows {
			return model.AlertEvent{}, false, nil
		}
		return model.AlertEvent{}, false, fmt.Errorf("scan unresolved alert: %w", err)
	}
	event.Resolved = resolved != 0
	event.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return event, true, nil
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

	if err := rows.Scan(&r.ID, &r.Metric, &r.Target, &r.Operator, &r.Threshold, &r.DurationS, &enabled, &createdStr, &updatedStr); err != nil {
		return model.AlertRule{}, fmt.Errorf("scan alert rule: %w", err)
	}

	r.Enabled = enabled != 0
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return r, nil
}

func (s *Service) validateRule(ctx context.Context, rule *model.AlertRule) error {
	switch rule.Metric {
	case "cpu", "ram", "disk", "load1", "load5", "load15":
		rule.Target = ""
	case "service_down":
		if rule.Target == "" {
			return model.NewValidationError("target is required for " + rule.Metric)
		}
		if s.services != nil {
			status, err := s.services.Status(ctx, rule.Target)
			if err != nil || status == nil {
				return model.NewValidationError("service target is not available")
			}
		}
	case "ssl_expiry":
		if rule.Target == "" {
			return model.NewValidationError("target is required for " + rule.Metric)
		}
		if s.certs != nil {
			certificates, err := s.certs.List(ctx)
			if err != nil {
				return model.NewValidationError("certificate targets are not available")
			}
			found := false
			for _, certificate := range certificates {
				if certificate.Domain == rule.Target && certificate.Status == "active" {
					found = true
					break
				}
			}
			if !found {
				return model.NewValidationError("active certificate target was not found")
			}
		}
	default:
		return model.NewValidationError("unsupported metric")
	}
	switch rule.Operator {
	case "gt", "lt", "eq":
	default:
		return model.NewValidationError("operator must be gt, lt, or eq")
	}
	if rule.DurationS < 0 {
		return model.NewValidationError("duration_s must be zero or greater")
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
