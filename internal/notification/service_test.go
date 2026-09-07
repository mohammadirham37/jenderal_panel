package notification

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateChannel(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	ctx := context.Background()

	ch, err := svc.CreateChannel(ctx, model.NotificationChannel{
		Type:    "webhook",
		Config:  `{"url":"https://example.com/hook"}`,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if ch.ID == "" {
		t.Error("expected non-empty ID")
	}
	if ch.Type != "webhook" {
		t.Errorf("expected type=webhook, got %s", ch.Type)
	}
	if !ch.Enabled {
		t.Error("expected enabled=true")
	}

	// Validation: empty type
	_, err = svc.CreateChannel(ctx, model.NotificationChannel{
		Config: `{"url":"https://example.com"}`,
	})
	if err == nil {
		t.Error("expected validation error for empty type")
	}

	// Validation: invalid type
	_, err = svc.CreateChannel(ctx, model.NotificationChannel{
		Type:   "sms",
		Config: `{}`,
	})
	if err == nil {
		t.Error("expected validation error for invalid type")
	}

	// Validation: empty config
	_, err = svc.CreateChannel(ctx, model.NotificationChannel{
		Type: "webhook",
	})
	if err == nil {
		t.Error("expected validation error for empty config")
	}

	// Validation: invalid JSON config
	_, err = svc.CreateChannel(ctx, model.NotificationChannel{
		Type:   "webhook",
		Config: "not json",
	})
	if err == nil {
		t.Error("expected validation error for invalid JSON config")
	}
}

func TestListChannels(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	ctx := context.Background()

	// Empty list
	channels, err := svc.ListChannels(ctx)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(channels))
	}

	// Create channels
	_, err = svc.CreateChannel(ctx, model.NotificationChannel{
		Type: "webhook", Config: `{"url":"https://a.com"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create 1: %v", err)
	}
	_, err = svc.CreateChannel(ctx, model.NotificationChannel{
		Type: "telegram", Config: `{"bot_token":"abc","chat_id":"123"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}

	channels, err = svc.ListChannels(ctx)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(channels))
	}
}

func TestGetChannel(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	ctx := context.Background()

	created, err := svc.CreateChannel(ctx, model.NotificationChannel{
		Type: "discord", Config: `{"webhook_url":"https://discord.com/hook"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.GetChannel(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, got.ID)
	}
	if got.Type != "discord" {
		t.Errorf("expected type=discord, got %s", got.Type)
	}

	// Not found
	_, err = svc.GetChannel(ctx, "nonexistent")
	if err == nil {
		t.Error("expected not found error")
	}
}

func TestUpdateAndDeleteChannel(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	ctx := context.Background()

	created, err := svc.CreateChannel(ctx, model.NotificationChannel{
		Type: "webhook", Config: `{"url":"https://old.com"}`, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Update
	err = svc.UpdateChannel(ctx, created.ID, model.NotificationChannel{
		Type: "webhook", Config: `{"url":"https://new.com"}`, Enabled: false,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := svc.GetChannel(ctx, created.ID)
	if got.Config != `{"url":"https://new.com"}` {
		t.Errorf("expected updated config, got %s", got.Config)
	}
	if got.Enabled {
		t.Error("expected enabled=false")
	}

	// Update non-existent
	err = svc.UpdateChannel(ctx, "nonexistent", model.NotificationChannel{
		Type: "webhook", Config: `{"url":"https://x.com"}`,
	})
	if err == nil {
		t.Error("expected not found error")
	}

	// Delete
	err = svc.DeleteChannel(ctx, created.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = svc.GetChannel(ctx, created.ID)
	if err == nil {
		t.Error("expected not found after delete")
	}

	// Delete non-existent
	err = svc.DeleteChannel(ctx, "nonexistent")
	if err == nil {
		t.Error("expected not found error")
	}
}
