package database

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/config"
)

func retentionDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.db")
	db, err := Open(config.DatabaseConfig{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func TestApplyRetentionTrimsUnboundedTables(t *testing.T) {
	db, _ := retentionDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -TaskHistoryDays-1).Format(time.RFC3339Nano)
	recent := now.AddDate(0, 0, -1).Format(time.RFC3339Nano)
	bigOutput := strings.Repeat("apt output line\n", 200)

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Old finished, recent finished, and an old task still running.
	mustExec(`INSERT INTO background_tasks (id, name, module, status, output, error, started_at, ended_at, updated_at)
		VALUES ('old', 'Install PHP', 'php', 'completed', ?, '', ?, ?, ?)`, bigOutput, old, old, old)
	mustExec(`INSERT INTO background_tasks (id, name, module, status, output, error, started_at, ended_at, updated_at)
		VALUES ('recent', 'Deploy', 'website', 'completed', ?, '', ?, ?, ?)`, bigOutput, recent, recent, recent)
	mustExec(`INSERT INTO background_tasks (id, name, module, status, output, error, started_at, ended_at, updated_at)
		VALUES ('running', 'Backup', 'backup', 'running', '', '', ?, NULL, ?)`, old, old)

	mustExec(`INSERT INTO websites (id, domain, document_root, web_user, created_at, updated_at)
		VALUES ('w1', 'shop.test', '/home/web_shop/public', 'web_shop', ?, ?)`, recent, recent)
	for i := 0; i < DeploymentHistoryPerSite+5; i++ {
		ts := now.AddDate(0, 0, -i).Format(time.RFC3339)
		mustExec(`INSERT INTO deployments (id, website_id, status, log, created_at, updated_at)
			VALUES (?, 'w1', 'success', ?, ?, ?)`, fmt.Sprintf("d%03d", i), bigOutput, ts, ts)
	}

	mustExec(`INSERT INTO users (id, username, email, password, is_active, created_at, updated_at)
		VALUES ('u1', 'alice', 'alice@test.com', 'x', 1, ?, ?)`, recent, recent)
	mustExec(`INSERT INTO audit_logs (id, user_id, action, module, target, detail, ip_address, created_at)
		VALUES ('a-old', 'u1', 'login', 'auth', 'x', 'y', '127.0.0.1', ?)`,
		now.AddDate(0, 0, -AuditRetentionDays-1).Format(time.RFC3339))
	mustExec(`INSERT INTO audit_logs (id, user_id, action, module, target, detail, ip_address, created_at)
		VALUES ('a-new', 'u1', 'login', 'auth', 'x', 'y', '127.0.0.1', ?)`, recent)

	report, err := ApplyRetention(ctx, db, now)
	if err != nil {
		t.Fatalf("ApplyRetention: %v", err)
	}
	if report.Tasks != 1 {
		t.Errorf("tasks removed = %d, want 1", report.Tasks)
	}
	if report.AuditEntries != 1 {
		t.Errorf("audit entries removed = %d, want 1", report.AuditEntries)
	}
	if report.DeploymentLogs != 5 {
		t.Errorf("deployment logs cleared = %d, want 5", report.DeploymentLogs)
	}

	var ids []string
	rows, err := db.Query(`SELECT id FROM background_tasks ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if strings.Join(ids, ",") != "recent,running" {
		t.Errorf("remaining tasks = %v, want the recent one and the running one", ids)
	}

	// The newest deployments keep their logs.
	var kept int
	if err := db.QueryRow(`SELECT COUNT(*) FROM deployments WHERE log IS NOT NULL`).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != DeploymentHistoryPerSite {
		t.Errorf("deployments with logs = %d, want %d", kept, DeploymentHistoryPerSite)
	}
}

func TestApplyRetentionCapsTaskCount(t *testing.T) {
	db, path := retentionDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < TaskHistoryMax+25; i++ {
		ts := now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		if _, err := db.Exec(`INSERT INTO background_tasks (id, name, module, status, output, error, started_at, ended_at, updated_at)
			VALUES (?, 'task', 'website', 'completed', ?, '', ?, ?, ?)`,
			fmt.Sprintf("t%04d", i), strings.Repeat("x", 4096), ts, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ApplyRetention(ctx, db, now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM background_tasks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != TaskHistoryMax {
		t.Errorf("tasks kept = %d, want %d", count, TaskHistoryMax)
	}

	before, after, err := Vacuum(ctx, db, path)
	if err != nil {
		t.Fatalf("Vacuum: %v", err)
	}
	if before == 0 || after == 0 {
		t.Fatalf("vacuum reported sizes before=%d after=%d", before, after)
	}
}
