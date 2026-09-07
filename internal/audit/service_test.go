package audit

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
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

func TestLogAndList(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	ctx := context.Background()

	err := svc.Log(ctx, LogEntry{
		Action: "login",
		Module: "auth",
		IP:     "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	err = svc.Log(ctx, LogEntry{
		Action: "reboot",
		Module: "server",
		Target: "server-1",
		Detail: "initiated reboot",
		IP:     "192.168.1.1",
	})
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	entries, total, err := svc.List(ctx, ListParams{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total 2, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	// Newest first
	if entries[0].Module != "server" {
		t.Errorf("expected newest entry module=server, got %s", entries[0].Module)
	}
}

func TestListWithModuleFilter(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	ctx := context.Background()

	_ = svc.Log(ctx, LogEntry{Action: "login", Module: "auth", IP: "127.0.0.1"})
	_ = svc.Log(ctx, LogEntry{Action: "reboot", Module: "server", IP: "127.0.0.1"})
	_ = svc.Log(ctx, LogEntry{Action: "logout", Module: "auth", IP: "127.0.0.1"})

	entries, total, err := svc.List(ctx, ListParams{Page: 1, PerPage: 10, Module: "auth"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total 2 for module=auth, got %d", total)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries for module=auth, got %d", len(entries))
	}
}

func TestPagination(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	ctx := context.Background()

	// Insert 15 entries
	for i := 0; i < 15; i++ {
		err := svc.Log(ctx, LogEntry{
			Action: "action",
			Module: "test",
			IP:     "127.0.0.1",
		})
		if err != nil {
			t.Fatalf("log %d: %v", i, err)
		}
	}

	// Page 1: 10 items
	entries, total, err := svc.List(ctx, ListParams{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if total != 15 {
		t.Errorf("expected total 15, got %d", total)
	}
	if len(entries) != 10 {
		t.Errorf("expected 10 entries on page 1, got %d", len(entries))
	}

	// Page 2: 5 items
	entries, total, err = svc.List(ctx, ListParams{Page: 2, PerPage: 10})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if total != 15 {
		t.Errorf("expected total 15, got %d", total)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries on page 2, got %d", len(entries))
	}
}

func TestDefaultParams(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db)
	ctx := context.Background()

	_ = svc.Log(ctx, LogEntry{Action: "test", Module: "test"})

	entries, total, err := svc.List(ctx, ListParams{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}
