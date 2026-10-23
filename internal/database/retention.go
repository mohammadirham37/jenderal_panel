package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

// Tables that grow with every panel action and were never trimmed: task
// output (whole apt/npm/composer logs), deployment logs, audit entries, and
// security events. Unbounded, they are the usual reason a panel VPS slowly
// fills up with no visible culprit, because they live inside one SQLite file.
const (
	// TaskHistoryDays keeps recent task output readable from the progress
	// views while bounding what an install-heavy server accumulates.
	TaskHistoryDays = 14
	// TaskHistoryMax caps the number of task rows regardless of age.
	TaskHistoryMax = 500
	// DeploymentHistoryPerSite keeps the newest deployments per website;
	// older ones lose only their log text, so the history stays.
	DeploymentHistoryPerSite = 50
	// AuditRetentionDays keeps audit entries long enough for review.
	AuditRetentionDays = 180
)

// RetentionReport counts what one retention pass removed.
type RetentionReport struct {
	Tasks            int64 `json:"tasks"`
	DeploymentLogs   int64 `json:"deployment_logs"`
	AuditEntries     int64 `json:"audit_entries"`
	ReclaimedBytes   int64 `json:"reclaimed_bytes"`
	DatabaseBytes    int64 `json:"database_bytes"`
	DatabaseBytesOld int64 `json:"database_bytes_before"`
}

// ApplyRetention trims the unbounded tables. It is safe to run at any time:
// nothing a running task needs is removed, and the counts let the caller
// report what was freed.
func ApplyRetention(ctx context.Context, db *sql.DB, now time.Time) (RetentionReport, error) {
	var report RetentionReport
	cutoff := now.UTC().AddDate(0, 0, -TaskHistoryDays).Format(time.RFC3339Nano)

	// Finished task rows older than the window, and anything beyond the cap.
	affected, err := execCount(ctx, db,
		`DELETE FROM background_tasks
		 WHERE status != 'running' AND updated_at < ?`, cutoff)
	if err != nil {
		return report, err
	}
	report.Tasks += affected

	affected, err = execCount(ctx, db,
		`DELETE FROM background_tasks WHERE id IN (
			SELECT id FROM background_tasks WHERE status != 'running'
			ORDER BY updated_at DESC LIMIT -1 OFFSET ?
		)`, TaskHistoryMax)
	if err != nil {
		return report, err
	}
	report.Tasks += affected

	// Deployment rows stay (the history is short and useful); only the log
	// text of older entries is dropped.
	affected, err = execCount(ctx, db,
		`UPDATE deployments SET log = NULL
		 WHERE log IS NOT NULL AND id NOT IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY website_id ORDER BY created_at DESC) AS rn
				FROM deployments
			) WHERE rn <= ?
		 )`, DeploymentHistoryPerSite)
	if err != nil {
		return report, err
	}
	report.DeploymentLogs = affected

	auditCutoff := now.UTC().AddDate(0, 0, -AuditRetentionDays).Format(time.RFC3339)
	affected, err = execCount(ctx, db, `DELETE FROM audit_logs WHERE created_at < ?`, auditCutoff)
	if err != nil {
		return report, err
	}
	report.AuditEntries = affected

	return report, nil
}

// Vacuum rebuilds the database file so deleted rows actually return their
// space to the filesystem, and reports the size before and after.
func Vacuum(ctx context.Context, db *sql.DB, path string) (before, after int64, err error) {
	before = databaseBytes(path)
	if _, err := db.ExecContext(ctx, `VACUUM`); err != nil {
		return before, before, fmt.Errorf("vacuum database: %w", err)
	}
	// In WAL mode the rebuilt pages sit in the write-ahead log until a
	// checkpoint folds them back, so the file on disk only shrinks here.
	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return before, databaseBytes(path), nil
	}
	return before, databaseBytes(path), nil
}

// databaseBytes is the on-disk footprint of the database: the file plus its
// write-ahead log sidecars.
func databaseBytes(path string) int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		total += fileSize(path + suffix)
	}
	return total
}

func execCount(ctx context.Context, db *sql.DB, query string, args ...any) (int64, error) {
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("retention: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// StartRetention trims the growth tables once at startup and daily after
// that, so a long-running panel does not accumulate task output, deployment
// logs, and audit entries forever.
func StartRetention(ctx context.Context, db *sql.DB, log func(RetentionReport, error)) {
	run := func() {
		report, err := ApplyRetention(ctx, db, time.Now())
		if log != nil {
			log(report, err)
		}
	}
	go func() {
		run()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
