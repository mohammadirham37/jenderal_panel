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
	schedules, err := s.svc.ListSchedules(ctx, SystemCaller)
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

		// Create the backup. The backup record carries kind=scheduled and the
		// service updates last_run_status when the task finishes.
		if _, err := s.svc.CreateScheduledBackup(ctx, sched.Type, sched.Target); err != nil {
			log.Printf("backup scheduler: create backup for schedule %s: %v", sched.ID, err)
			continue
		}
	}

	// Cleanup old backups based on retention.
	s.cleanup(ctx, schedules, now)
}

// cleanup deletes completed backups that exceeded their schedule's retention,
// expressed as a maximum age in days and/or a maximum number to keep.
func (s *Scheduler) cleanup(ctx context.Context, schedules []model.BackupSchedule, now time.Time) {
	pruned := pruneExpired(ctx, s.svc, schedules, now)
	if pruned > 0 {
		log.Printf("backup scheduler: pruned %d expired backups", pruned)
	}
}

// pruneExpired deletes completed backups past their retention window and
// returns how many were removed. Both limits apply per type/target: a backup
// is deleted when it is older than retention_days (when set) or no longer
// among the newest retention_keep (when set).
func pruneExpired(ctx context.Context, svc *Service, schedules []model.BackupSchedule, now time.Time) int {
	type key struct{ typ, target string }
	type limits struct {
		days int
		keep int
	}
	retention := make(map[key]limits)

	for _, sched := range schedules {
		k := key{sched.Type, sched.Target}
		retention[k] = limits{days: sched.RetentionDays, keep: sched.RetentionKeep}
	}

	backups, err := svc.List(ctx)
	if err != nil {
		log.Printf("backup scheduler: list backups for cleanup: %v", err)
		return 0
	}

	// Newest-first per key so keep-N keeps the newest entries.
	seen := make(map[key]int)
	pruned := 0
	for _, b := range backups {
		if b.Status != "completed" || b.Kind == KindSafety {
			continue
		}

		k := key{b.Type, b.Target}
		lim, ok := retention[k]
		if !ok {
			continue
		}

		seen[k]++
		if retentionExpired(lim.days, lim.keep, seen[k], b.CreatedAt, now) {
			if err := svc.DeleteBackup(ctx, b.ID); err != nil {
				log.Printf("backup scheduler: delete expired backup %s: %v", b.ID, err)
				continue
			}
			pruned++
		}
	}
	return pruned
}

// retentionExpired reports whether a completed backup should be pruned:
// rank is its 1-based position among the newest backups of the same
// type/target. A backup is expired when it is older than days (when set) or
// pushed out of the newest keep (when set). Safety backups are never pruned
// here.
func retentionExpired(days, keep, rank int, createdAt, now time.Time) bool {
	if days > 0 && now.Sub(createdAt) > time.Duration(days)*24*time.Hour {
		return true
	}
	if keep > 0 && rank > keep {
		return true
	}
	return false
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
