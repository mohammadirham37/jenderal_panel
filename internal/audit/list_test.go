package audit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
)

// Entries written without a user (background jobs, system actions) store a
// NULL user_id; listing them used to fail the whole audit log page.
func TestListIncludesEntriesWithoutAUser(t *testing.T) {
	db, err := database.Open(config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "panel.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	ctx := context.Background()
	if err := svc.Log(ctx, LogEntry{Action: "auto_renew", Module: "ssl", Target: "shop.test"}); err != nil {
		t.Fatal(err)
	}

	entries, total, err := svc.List(ctx, ListParams{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(entries) != 1 || entries[0].UserID != "" || entries[0].Action != "auto_renew" {
		t.Errorf("unexpected entries %+v (total %d)", entries, total)
	}
}
