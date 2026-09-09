package database

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrateCreatesCoreTables(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	for _, table := range []string{
		"users", "roles", "permissions", "user_roles", "role_permissions", "sessions",
		"audit_logs", "settings", "server_metrics", "alert_rule_targets", "websites", "background_tasks",
		"security_settings", "security_events", "security_event_occurrences", "security_manual_bans",
	} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestMigrateCanRunTwiceWithAlterTableMigration(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE filename = '019_website_profiles.sql'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("019 migration applications = %d, want 1", applied)
	}
}

func TestMigratePreservesLegacyWebsiteAndAddsProfileDefaults(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	legacySchema, err := migrationsFS.ReadFile("migrations/007_websites.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(legacySchema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO websites
		(id, domain, app_type, php_version, document_root, web_user, status, ssl_enabled, created_at, updated_at)
		VALUES ('legacy', 'legacy.example.com', 'laravel', '8.2', '/srv/legacy/public', 'web_legacy', 'active', 0, '2026-09-09T00:00:00Z', '2026-09-09T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var appType, documentRoot, framework, variant, setupMode, stage, log string
	if err := db.QueryRow(`SELECT app_type, document_root, framework, project_variant, setup_mode, provision_stage, provision_log
		FROM websites WHERE id = 'legacy'`).Scan(&appType, &documentRoot, &framework, &variant, &setupMode, &stage, &log); err != nil {
		t.Fatal(err)
	}
	if appType != "laravel" || documentRoot != "/srv/legacy/public" {
		t.Fatalf("legacy values changed: app_type=%q document_root=%q", appType, documentRoot)
	}
	if framework != "none" || variant != "empty" || setupMode != "config-only" || stage != "" || log != "" {
		t.Fatalf("unexpected defaults: framework=%q variant=%q setup_mode=%q stage=%q log=%q", framework, variant, setupMode, stage, log)
	}
}

func TestMigrateAddsCompatibilityNodeVersionWithoutChangingExistingWebsite(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	legacySchema, err := migrationsFS.ReadFile("migrations/007_websites.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(legacySchema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO websites
		(id, domain, app_type, php_version, document_root, web_user, status, ssl_enabled, created_at, updated_at)
		VALUES ('legacy-node', 'node.example.com', 'nodejs', NULL, '/home/web_node_example_com/public', 'web_node_example_com', 'active', 1, '2026-09-09T00:00:00Z', '2026-09-09T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	var domain, documentRoot, status, nodeVersion string
	var sslEnabled int
	if err := db.QueryRow(`SELECT domain, document_root, status, ssl_enabled, node_version FROM websites WHERE id = 'legacy-node'`).
		Scan(&domain, &documentRoot, &status, &sslEnabled, &nodeVersion); err != nil {
		t.Fatal(err)
	}
	if domain != "node.example.com" || documentRoot != "/home/web_node_example_com/public" || status != "active" || sslEnabled != 1 {
		t.Fatalf("legacy website changed: domain=%q root=%q status=%q ssl=%d", domain, documentRoot, status, sslEnabled)
	}
	if nodeVersion != "24" {
		t.Fatalf("node_version = %q, want compatibility default 24", nodeVersion)
	}
}
