package backup

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Scheduler runs scheduled backups and retention cleanup in the background.
type Scheduler struct {
	svc *Service
}

// NewScheduler creates a new Scheduler.
func NewScheduler(svc *Service) *Scheduler {
	return &Scheduler{svc: svc}
}

// Start launches a background goroutine that checks schedules every hour.
// It blocks until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Scheduler) loop(ctx context.Context) {
	// Run once immediately on start, then every hour.
	s.tick(ctx)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	schedules, err := s.svc.ListSchedules(ctx)
	if err != nil {
		log.Printf("backup scheduler: list schedules: %v", err)
		return
	}

	now := time.Now().UTC()

	for _, sched := range schedules {
		if !sched.Enabled {
			continue
		}

		if !shouldRun(sched.Schedule, sched.LastRun, now) {
			continue
		}

		// Create the backup.
		_, err := s.svc.CreateBackup(ctx, sched.Type, sched.Target)
		if err != nil {
			log.Printf("backup scheduler: create backup for schedule %s: %v", sched.ID, err)
			continue
		}

		// Update last_run.
		nowStr := now.Format(time.RFC3339)
		_, _ = s.svc.db.ExecContext(ctx,
			`UPDATE backup_schedules SET last_run = ?, updated_at = ? WHERE id = ?`,
			nowStr, nowStr, sched.ID,
		)
	}

	// Cleanup old backups based on retention.
	s.cleanup(ctx, schedules, now)
}

// cleanup deletes completed backups older than the retention period.
func (s *Scheduler) cleanup(ctx context.Context, schedules []model.BackupSchedule, now time.Time) {
	// Find the minimum retention across all schedules per type/target.
	type key struct{ typ, target string }
	retention := make(map[key]int)

	for _, sched := range schedules {
		k := key{sched.Type, sched.Target}
		if existing, ok := retention[k]; !ok || sched.RetentionDays < existing {
			retention[k] = sched.RetentionDays
		}
	}

	// Query completed backups and check age.
	backups, err := s.svc.List(ctx)
	if err != nil {
		log.Printf("backup scheduler: list backups for cleanup: %v", err)
		return
	}

	for _, b := range backups {
		if b.Status != "completed" {
			continue
		}

		k := key{b.Type, b.Target}
		days, ok := retention[k]
		if !ok {
			continue
		}

		age := now.Sub(b.CreatedAt)
		if age > time.Duration(days)*24*time.Hour {
			if err := s.svc.DeleteBackup(ctx, b.ID); err != nil {
				log.Printf("backup scheduler: delete expired backup %s: %v", b.ID, err)
			}
		}
	}
}

// shouldRun determines whether a schedule should run based on the schedule
// preset string and the time since last run.
//
// Supported schedule formats:
//   - "daily" or "0 0 * * *"    — once per day (24h interval)
//   - "weekly" or "0 0 * * 0"   — once per week (168h interval)
//   - "monthly" or "0 0 1 * *"  — once per month (720h interval)
//   - "hourly" or "0 * * * *"   — once per hour (1h interval)
//
// For unrecognised formats, defaults to daily.
func shouldRun(schedule string, lastRun time.Time, now time.Time) bool {
	interval := parseInterval(schedule)

	// If never run, run now.
	if lastRun.IsZero() {
		return true
	}

	return now.Sub(lastRun) >= interval
}

func parseInterval(schedule string) time.Duration {
	s := strings.TrimSpace(strings.ToLower(schedule))

	switch s {
	case "hourly", "0 * * * *":
		return 1 * time.Hour
	case "daily", "0 0 * * *":
		return 24 * time.Hour
	case "weekly", "0 0 * * 0":
		return 7 * 24 * time.Hour
	case "monthly", "0 0 1 * *":
		return 30 * 24 * time.Hour
	}

	// Default to daily for unrecognised formats.
	return 24 * time.Hour
}
