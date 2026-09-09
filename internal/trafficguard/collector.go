package trafficguard

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/security"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxCollectBytes = 10 * 1024 * 1024

var trafficWebUserPattern = regexp.MustCompile(`^web_[a-z0-9_]+$`)

type Collector struct {
	db     *sql.DB
	repo   *Repository
	exec   executor.CommandExecutor
	events *security.EventService
}

func NewCollector(db *sql.DB, repo *Repository, exec executor.CommandExecutor, events *security.EventService) *Collector {
	return &Collector{db: db, repo: repo, exec: exec, events: events}
}
func (c *Collector) Collect(ctx context.Context, now time.Time) error {
	rows, err := c.db.QueryContext(ctx, `SELECT id,web_user FROM websites WHERE status IN ('active','suspended')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, user string
		if err := rows.Scan(&id, &user); err != nil {
			return err
		}
		if !trafficWebUserPattern.MatchString(user) {
			return fmt.Errorf("website %s has an invalid web user", id)
		}
		if err := c.repo.EnsureProfile(ctx, id, now); err != nil {
			return err
		}
		if err := c.collectSite(ctx, id, "/home/"+user+"/logs/access.log", now); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return c.repo.RollupAndCleanup(ctx, now)
}
func (c *Collector) collectSite(ctx context.Context, id, path string, now time.Time) error {
	stat, err := c.exec.RunSudo(ctx, "/usr/bin/stat", "-c", "%i|%s", "--", path)
	if err != nil {
		return err
	}
	if stat.ExitCode != 0 {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(stat.Stdout), "|")
	if len(parts) != 2 {
		return fmt.Errorf("unexpected access log stat")
	}
	inode, _ := strconv.ParseUint(parts[0], 10, 64)
	size, _ := strconv.ParseInt(parts[1], 10, 64)
	cursor, err := c.repo.Cursor(ctx, id)
	if err != nil {
		return err
	}
	offset := NextOffset(cursor, inode, size)
	if offset == size {
		return nil
	}
	read, err := c.exec.RunSudo(ctx, "/usr/bin/dd", "if="+path, "skip="+strconv.FormatInt(offset, 10), "count="+strconv.Itoa(maxCollectBytes), "iflag=skip_bytes,count_bytes", "status=none")
	if err != nil {
		return err
	}
	if read.ExitCode != 0 {
		return fmt.Errorf("read access log: %s", strings.TrimSpace(read.Stderr))
	}
	data := read.Stdout
	consumed := 0
	if i := strings.LastIndexByte(data, '\n'); i >= 0 {
		data = data[:i+1]
	} else if len(data) < maxCollectBytes {
		return nil
	}
	buckets := map[time.Time]*MinuteBucket{}
	seconds := map[time.Time]map[int64]int{}
	lines := 0
	for _, rawLine := range strings.SplitAfter(data, "\n") {
		if rawLine == "" {
			continue
		}
		if lines == 100000 {
			break
		}
		lines++
		consumed += len(rawLine)
		line := strings.TrimSuffix(strings.TrimSuffix(rawLine, "\n"), "\r")
		entry, e := ParseCombinedLog(line)
		if e != nil {
			continue
		}
		minute := entry.Time.Truncate(time.Minute)
		b := buckets[minute]
		if b == nil {
			b = &MinuteBucket{WebsiteID: id, BucketAt: minute, TopIPs: map[string]int{}, TopPaths: map[string]int{}, TopAgents: map[string]int{}}
			buckets[minute] = b
			seconds[minute] = map[int64]int{}
		}
		b.Requests++
		b.Bytes += entry.Bytes
		if entry.Status >= 400 && entry.Status < 500 {
			b.Status4xx++
		}
		if entry.Status >= 500 {
			b.Status5xx++
		}
		if entry.Status == 429 {
			b.Status429++
		}
		boundedCount(b.TopIPs, entry.IP.String())
		boundedCount(b.TopPaths, entry.Path)
		boundedCount(b.TopAgents, entry.UserAgent)
		seconds[minute][entry.Time.Unix()]++
	}
	var list []MinuteBucket
	for minute, b := range buckets {
		for _, n := range seconds[minute] {
			if n > b.PeakRPS {
				b.PeakRPS = n
			}
		}
		list = append(list, *b)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].BucketAt.Before(list[j].BucketAt) })
	base, err := c.repo.Baseline(ctx, id)
	if err != nil {
		return err
	}
	completedBefore := now.Truncate(time.Minute)
	for _, b := range list {
		if !b.BucketAt.Before(completedBefore) {
			continue
		}
		anomalies := Evaluate(base, b)
		if len(anomalies) == 0 {
			base = UpdateBaseline(base, b)
		}
		for _, a := range anomalies {
			c.record(ctx, id, a, now)
		}
	}
	if base.SampleCount > 0 {
		if err := c.repo.SaveBaseline(ctx, base); err != nil {
			return err
		}
	}
	return c.repo.CommitBuckets(ctx, list, LogCursor{WebsiteID: id, Inode: inode, Offset: offset + int64(consumed), UpdatedAt: now})
}
func boundedCount(values map[string]int, key string) {
	if key == "" {
		return
	}
	if _, ok := values[key]; ok {
		values[key]++
		return
	}
	if len(values) < 20 {
		values[key] = 1
		return
	}
	for existing, count := range values {
		if count <= 1 {
			delete(values, existing)
		} else {
			values[existing] = count - 1
		}
	}
}
func (c *Collector) record(ctx context.Context, id string, a Anomaly, now time.Time) {
	if c.events == nil {
		return
	}
	severity := security.SeverityMedium
	if a.Severity == "critical" {
		severity = security.SeverityCritical
	}
	_, _, _ = c.events.Record(ctx, security.EventInput{Fingerprint: "traffic:" + id + ":" + a.Signal, Category: "traffic", Severity: severity, Component: "traffic_guard", Resource: id, Evidence: fmt.Sprintf(`{"value":%.0f,"threshold":%.0f}`, a.Value, a.Threshold), RecommendedAction: "Review top clients and paths; use Traffic Guard Observe data before enabling HTTP limits."}, now)
}
