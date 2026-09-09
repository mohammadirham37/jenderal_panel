package taskrunner

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Store interface {
	Upsert(context.Context, Task) error
	LoadRecent(context.Context, int) ([]Task, error)
	FailRunning(context.Context, time.Time, string) error
}

type sqliteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) Store {
	return &sqliteStore{db: db}
}

func (s *sqliteStore) Upsert(ctx context.Context, task Task) error {
	var endedAt any
	if !task.EndedAt.IsZero() {
		endedAt = task.EndedAt.Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO background_tasks
		(id, name, module, status, output, error, started_at, ended_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			module = excluded.module,
			status = excluded.status,
			output = excluded.output,
			error = excluded.error,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at,
			updated_at = excluded.updated_at`,
		task.ID, task.Name, task.Module, task.Status, task.Output, task.Error,
		task.StartedAt.Format(time.RFC3339Nano), endedAt, task.UpdatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *sqliteStore) LoadRecent(ctx context.Context, limit int) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, module, status, output, error,
		started_at, ended_at, updated_at
		FROM background_tasks ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		var startedAt, updatedAt string
		var endedAt sql.NullString
		if err := rows.Scan(&task.ID, &task.Name, &task.Module, &task.Status, &task.Output,
			&task.Error, &startedAt, &endedAt, &updatedAt); err != nil {
			return nil, err
		}
		if task.StartedAt, err = parseTaskTime(startedAt); err != nil {
			return nil, fmt.Errorf("parse task %s started_at: %w", task.ID, err)
		}
		if task.UpdatedAt, err = parseTaskTime(updatedAt); err != nil {
			return nil, fmt.Errorf("parse task %s updated_at: %w", task.ID, err)
		}
		if endedAt.Valid {
			if task.EndedAt, err = parseTaskTime(endedAt.String); err != nil {
				return nil, fmt.Errorf("parse task %s ended_at: %w", task.ID, err)
			}
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *sqliteStore) FailRunning(ctx context.Context, now time.Time, message string) error {
	formatted := now.Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE background_tasks
		SET status = 'failed', error = ?, ended_at = ?, updated_at = ?
		WHERE status = 'running'`, message, formatted, formatted)
	return err
}

func parseTaskTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}
