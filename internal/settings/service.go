package settings

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) GetAll(ctx context.Context) ([]model.Setting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()

	var settings []model.Setting
	for rows.Next() {
		var st model.Setting
		var updatedAt string
		if err := rows.Scan(&st.Key, &st.Value, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		st.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		settings = append(settings, st)
	}
	return settings, rows.Err()
}

func (s *Service) Set(ctx context.Context, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, now,
	)
	if err != nil {
		return fmt.Errorf("upsert setting: %w", err)
	}
	return nil
}
