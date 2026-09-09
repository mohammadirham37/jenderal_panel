package database

import (
	"database/sql"
	"testing"
)

func TestMigrate(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}

	tables := []string{
		"users", "roles", "permissions", "user_roles",
		"role_permissions", "sessions", "audit_logs",
		"settings", "server_metrics", "alert_rule_targets",
	}

	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate() error: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate() error: %v", err)
	}
}
