package security

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const (
	defaultEventRetentionDays = 90
	notificationCooldown      = 15 * time.Minute
)

type NotificationSender interface {
	SendAll(context.Context, string) error
}

type EventService struct {
	db       *sql.DB
	notifier NotificationSender
}

func NewEventService(db *sql.DB, notifier NotificationSender) *EventService {
	return &EventService{db: db, notifier: notifier}
}

func (s *EventService) Record(ctx context.Context, input EventInput, now time.Time) (Event, bool, error) {
	if err := validateEventInput(input); err != nil {
		return Event{}, false, err
	}
	now = now.UTC()
	timestamp := now.Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, false, fmt.Errorf("begin security event: %w", err)
	}
	defer tx.Rollback()

	event, err := findActiveEvent(ctx, tx, input.Fingerprint)
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !created {
		return Event{}, false, fmt.Errorf("find matching security event: %w", err)
	}

	shouldNotify := false
	if created {
		event = Event{
			ID:              ulid.Make().String(),
			EventInput:      input,
			Status:          StatusOpen,
			OccurrenceCount: 1,
			FirstSeen:       now,
			LastSeen:        now,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		shouldNotify = input.Severity == SeverityHigh || input.Severity == SeverityCritical
		if shouldNotify {
			notifiedAt := now
			event.NotifiedAt = &notifiedAt
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO security_events
			(id, fingerprint, category, severity, component, resource, evidence, recommended_action,
			 status, occurrence_count, first_seen, last_seen, notified_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.ID, input.Fingerprint, input.Category, input.Severity, input.Component,
			input.Resource, input.Evidence, input.RecommendedAction, event.Status,
			event.OccurrenceCount, timestamp, timestamp, nullableTime(event.NotifiedAt), timestamp, timestamp); err != nil {
			return Event{}, false, fmt.Errorf("insert security event: %w", err)
		}
	} else {
		event.EventInput = input
		event.OccurrenceCount++
		event.LastSeen = now
		event.UpdatedAt = now
		shouldNotify = (input.Severity == SeverityHigh || input.Severity == SeverityCritical) &&
			(event.NotifiedAt == nil || now.Sub(*event.NotifiedAt) >= notificationCooldown)
		if shouldNotify {
			notifiedAt := now
			event.NotifiedAt = &notifiedAt
		}
		if _, err := tx.ExecContext(ctx, `UPDATE security_events SET
			category = ?, severity = ?, component = ?, resource = ?, evidence = ?,
			recommended_action = ?, occurrence_count = ?, last_seen = ?, notified_at = ?, updated_at = ?
			WHERE id = ?`, input.Category, input.Severity, input.Component, input.Resource,
			input.Evidence, input.RecommendedAction, event.OccurrenceCount, timestamp,
			nullableTime(event.NotifiedAt), timestamp, event.ID); err != nil {
			return Event{}, false, fmt.Errorf("update security event: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO security_event_occurrences
		(id, event_id, evidence, observed_at) VALUES (?, ?, ?, ?)`,
		ulid.Make().String(), event.ID, input.Evidence, timestamp); err != nil {
		return Event{}, false, fmt.Errorf("insert security occurrence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Event{}, false, fmt.Errorf("commit security event: %w", err)
	}

	if shouldNotify && s.notifier != nil {
		_ = s.notifier.SendAll(ctx, formatEventNotification(event))
	}
	return event, created, nil
}

func (s *EventService) List(ctx context.Context, filter EventFilter) ([]Event, int, error) {
	if filter.Status != "" && !validStatus(filter.Status) {
		return nil, 0, model.NewValidationError("invalid event status")
	}
	if filter.Severity != "" && !validSeverity(filter.Severity) {
		return nil, 0, model.NewValidationError("invalid event severity")
	}
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		return nil, 0, model.NewValidationError("offset must not be negative")
	}

	where, args := eventFilterSQL(filter)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM security_events`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count security events: %w", err)
	}
	queryArgs := append(append([]any{}, args...), filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventColumns+` FROM security_events`+where+
		` ORDER BY last_seen DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list security events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	return events, total, rows.Err()
}

func (s *EventService) Transition(ctx context.Context, id string, status EventStatus, now time.Time) error {
	if strings.TrimSpace(id) == "" {
		return model.NewValidationError("event id is required")
	}
	if !validStatus(status) {
		return model.NewValidationError("invalid event status")
	}
	var current EventStatus
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM security_events WHERE id = ?`, id).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return fmt.Errorf("load security event status: %w", err)
	}
	if !validTransition(current, status) {
		return model.NewValidationError(fmt.Sprintf("cannot transition event from %s to %s", current, status))
	}
	result, err := s.db.ExecContext(ctx, `UPDATE security_events SET status = ?, updated_at = ? WHERE id = ?`,
		status, now.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("transition security event: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (s *EventService) Cleanup(ctx context.Context, now time.Time) error {
	days := defaultEventRetentionDays
	var configured string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM security_settings WHERE key = 'security.event_retention_days'`).Scan(&configured); err == nil {
		if parsed, parseErr := strconv.Atoi(configured); parseErr == nil && parsed >= 1 && parsed <= 3650 {
			days = parsed
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("load security event retention: %w", err)
	}
	cutoff := now.UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin security event cleanup: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM security_event_occurrences WHERE observed_at < ?
		AND event_id IN (SELECT id FROM security_events WHERE status IN ('resolved','false_positive') AND last_seen < ?)`, cutoff, cutoff); err != nil {
		return fmt.Errorf("delete old security occurrences: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM security_events
		WHERE status IN ('resolved','false_positive') AND last_seen < ?`, cutoff); err != nil {
		return fmt.Errorf("delete old security events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit security event cleanup: %w", err)
	}
	return nil
}

const eventColumns = `id, fingerprint, category, severity, component, resource, evidence,
	recommended_action, status, occurrence_count, first_seen, last_seen, notified_at, created_at, updated_at`

type rowScanner interface {
	Scan(...any) error
}

func findActiveEvent(ctx context.Context, tx *sql.Tx, fingerprint string) (Event, error) {
	return scanEvent(tx.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM security_events
		WHERE fingerprint = ? AND status IN ('open','acknowledged') ORDER BY last_seen DESC LIMIT 1`, fingerprint))
}

func scanEvent(row rowScanner) (Event, error) {
	var event Event
	var firstSeen, lastSeen, createdAt, updatedAt string
	var notifiedAt sql.NullString
	err := row.Scan(&event.ID, &event.Fingerprint, &event.Category, &event.Severity,
		&event.Component, &event.Resource, &event.Evidence, &event.RecommendedAction,
		&event.Status, &event.OccurrenceCount, &firstSeen, &lastSeen, &notifiedAt,
		&createdAt, &updatedAt)
	if err != nil {
		return Event{}, err
	}
	if event.FirstSeen, err = parseEventTime(firstSeen); err != nil {
		return Event{}, fmt.Errorf("parse first_seen: %w", err)
	}
	if event.LastSeen, err = parseEventTime(lastSeen); err != nil {
		return Event{}, fmt.Errorf("parse last_seen: %w", err)
	}
	if event.CreatedAt, err = parseEventTime(createdAt); err != nil {
		return Event{}, fmt.Errorf("parse created_at: %w", err)
	}
	if event.UpdatedAt, err = parseEventTime(updatedAt); err != nil {
		return Event{}, fmt.Errorf("parse updated_at: %w", err)
	}
	if notifiedAt.Valid {
		parsed, parseErr := parseEventTime(notifiedAt.String)
		if parseErr != nil {
			return Event{}, fmt.Errorf("parse notified_at: %w", parseErr)
		}
		event.NotifiedAt = &parsed
	}
	return event, nil
}

func validateEventInput(input EventInput) error {
	if strings.TrimSpace(input.Fingerprint) == "" {
		return model.NewValidationError("fingerprint is required")
	}
	if strings.TrimSpace(input.Category) == "" {
		return model.NewValidationError("category is required")
	}
	if !validSeverity(input.Severity) {
		return model.NewValidationError("invalid event severity")
	}
	if strings.TrimSpace(input.Component) == "" {
		return model.NewValidationError("component is required")
	}
	return nil
}

func eventFilterSQL(filter EventFilter) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if filter.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Severity != "" {
		clauses = append(clauses, "severity = ?")
		args = append(args, filter.Severity)
	}
	if filter.Component != "" {
		clauses = append(clauses, "component = ?")
		args = append(args, filter.Component)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func validTransition(from, to EventStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusOpen:
		return to == StatusAcknowledged || to == StatusResolved || to == StatusFalsePositive
	case StatusAcknowledged:
		return to == StatusResolved || to == StatusFalsePositive
	default:
		return false
	}
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseEventTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

func formatEventNotification(event Event) string {
	return fmt.Sprintf("Jenderal Panel security event [%s]: %s on %s (%s). %s",
		strings.ToUpper(string(event.Severity)), event.Category, event.Resource, event.Component, event.RecommendedAction)
}
