package notification

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Service manages notification channels and dispatches messages.
type Service struct {
	db *sql.DB
}

// NewService creates a new notification Service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// CreateChannel inserts a new notification channel.
func (s *Service) CreateChannel(ctx context.Context, ch model.NotificationChannel) (model.NotificationChannel, error) {
	if ch.Type == "" {
		return model.NotificationChannel{}, model.NewValidationError("type is required")
	}
	switch ch.Type {
	case "email", "telegram", "discord", "webhook":
	default:
		return model.NotificationChannel{}, model.NewValidationError("type must be email, telegram, discord, or webhook")
	}
	if err := ValidateConfig(ch.Type, ch.Config); err != nil {
		return model.NotificationChannel{}, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	ch.ID = ulid.Make().String()
	ch.CreatedAt = now
	ch.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_channels (id, type, config, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ch.ID, ch.Type, ch.Config, boolToInt(ch.Enabled), nowStr, nowStr,
	)
	if err != nil {
		return model.NotificationChannel{}, fmt.Errorf("insert notification channel: %w", err)
	}

	return ch, nil
}

// UpdateChannel updates an existing notification channel.
func (s *Service) UpdateChannel(ctx context.Context, id string, ch model.NotificationChannel) error {
	if ch.Type == "" {
		return model.NewValidationError("type is required")
	}
	switch ch.Type {
	case "email", "telegram", "discord", "webhook":
	default:
		return model.NewValidationError("type must be email, telegram, discord, or webhook")
	}
	if err := ValidateConfig(ch.Type, ch.Config); err != nil {
		return err
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)

	res, err := s.db.ExecContext(ctx,
		`UPDATE notification_channels SET type=?, config=?, enabled=?, updated_at=?
		 WHERE id=?`,
		ch.Type, ch.Config, boolToInt(ch.Enabled), nowStr, id,
	)
	if err != nil {
		return fmt.Errorf("update notification channel: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// DeleteChannel removes a notification channel by ID.
func (s *Service) DeleteChannel(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete notification channel: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

// ListChannels returns all notification channels.
func (s *Service) ListChannels(ctx context.Context) ([]model.NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, config, enabled, created_at, updated_at
		 FROM notification_channels ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query notification channels: %w", err)
	}
	defer rows.Close()

	var channels []model.NotificationChannel
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// GetChannel returns a single notification channel by ID.
func (s *Service) GetChannel(ctx context.Context, id string) (model.NotificationChannel, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, type, config, enabled, created_at, updated_at
		 FROM notification_channels WHERE id=?`, id)

	var ch model.NotificationChannel
	var enabled int
	var createdStr, updatedStr string

	err := row.Scan(&ch.ID, &ch.Type, &ch.Config, &enabled, &createdStr, &updatedStr)
	if err == sql.ErrNoRows {
		return model.NotificationChannel{}, model.ErrNotFound
	}
	if err != nil {
		return model.NotificationChannel{}, fmt.Errorf("scan notification channel: %w", err)
	}

	ch.Enabled = enabled != 0
	ch.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	ch.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return ch, nil
}

// TestChannel sends a test message through the specified channel.
func (s *Service) TestChannel(ctx context.Context, id string) error {
	ch, err := s.GetChannel(ctx, id)
	if err != nil {
		return err
	}

	return dispatch(ctx, ch.Type, ch.Config, "Jenderal Panel test notification")
}

// SendAll sends a message to all enabled notification channels.
func (s *Service) SendAll(ctx context.Context, message string) error {
	channels, err := s.ListChannels(ctx)
	if err != nil {
		return err
	}

	var lastErr error
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		if err := dispatch(ctx, ch.Type, ch.Config, message); err != nil {
			log.Printf("[notification] failed to send via %s channel %s: %v", ch.Type, ch.ID, err)
			lastErr = err
		}
	}
	return lastErr
}

// dispatch routes a message to the appropriate channel sender.
func dispatch(ctx context.Context, channelType, config, message string) error {
	switch channelType {
	case "webhook":
		return sendWebhook(ctx, config, message)
	case "telegram":
		return sendTelegram(ctx, config, message)
	case "discord":
		return sendDiscord(ctx, config, message)
	case "email":
		return sendEmailWithDialer(ctx, config, message, networkSMTPDialer{})
	default:
		return fmt.Errorf("unsupported channel type: %s", channelType)
	}
}

func scanChannel(rows *sql.Rows) (model.NotificationChannel, error) {
	var ch model.NotificationChannel
	var enabled int
	var createdStr, updatedStr string

	if err := rows.Scan(&ch.ID, &ch.Type, &ch.Config, &enabled, &createdStr, &updatedStr); err != nil {
		return model.NotificationChannel{}, fmt.Errorf("scan notification channel: %w", err)
	}

	ch.Enabled = enabled != 0
	ch.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	ch.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return ch, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
