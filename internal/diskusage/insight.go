package diskusage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// The panel's own state was not part of the usage report, so growth inside
// the SQLite database (task output, audit entries) was invisible. These
// additions make the page answer three questions: what is the panel itself
// using, which files are the biggest, and what grew since last week.

// TableUsage is the measured footprint of one database table.
type TableUsage struct {
	Name  string `json:"name"`
	Rows  int64  `json:"rows"`
	Bytes int64  `json:"bytes"`
}

// PanelDataUsage reports the panel's own storage: the database file, its
// largest tables, and the state directory around it.
type PanelDataUsage struct {
	DatabasePath  string       `json:"database_path"`
	DatabaseBytes int64        `json:"database_bytes"`
	StateBytes    int64        `json:"state_bytes"`
	Tables        []TableUsage `json:"tables"`
}

// growthTables are the tables worth reporting: the ones that grow with
// panel activity rather than with the number of websites.
var growthTables = []string{
	"background_tasks", "audit_logs", "server_metrics", "security_events",
	"deployments", "bandwidth_stats", "malware_findings",
}

// SetDatabasePath tells the service where the SQLite file lives so it can
// report its size.
func (s *Service) SetDatabasePath(path string) { s.dbPath = path }

// PanelData measures the panel's own storage. Missing pieces are reported
// as zero rather than failing the whole page.
func (s *Service) PanelData(ctx context.Context) PanelDataUsage {
	usage := PanelDataUsage{DatabasePath: s.dbPath}
	if s.dbPath != "" {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if info, err := os.Stat(s.dbPath + suffix); err == nil {
				usage.DatabaseBytes += info.Size()
			}
		}
		if res, err := s.exec.RunSudo(ctx, "du", "-sb", filepath.Dir(s.dbPath)); err == nil && res != nil {
			for path, bytes := range parseDuSizes(res.Stdout) {
				if path == filepath.Dir(s.dbPath) {
					usage.StateBytes = bytes
				}
			}
		}
	}
	if s.db == nil {
		return usage
	}

	pageSize := s.scalar(ctx, `PRAGMA page_size`)
	for _, table := range growthTables {
		rows := s.scalar(ctx, `SELECT COUNT(*) FROM `+table)
		if rows < 0 {
			continue // table not present in this schema version
		}
		entry := TableUsage{Name: table, Rows: rows}
		// dbstat is the exact answer when the build has it; otherwise the
		// stored text length is a close, cheap estimate.
		if bytes := s.scalar(ctx, `SELECT SUM(pgsize) FROM dbstat WHERE name = ?`, table); bytes > 0 {
			entry.Bytes = bytes
		} else {
			entry.Bytes = s.estimateTableBytes(ctx, table, rows, pageSize)
		}
		usage.Tables = append(usage.Tables, entry)
	}
	sort.SliceStable(usage.Tables, func(i, j int) bool { return usage.Tables[i].Bytes > usage.Tables[j].Bytes })
	return usage
}

// estimateTableBytes approximates a table's size from the text it stores.
func (s *Service) estimateTableBytes(ctx context.Context, table string, rows, pageSize int64) int64 {
	var sum int64
	switch table {
	case "background_tasks":
		sum = s.scalar(ctx, `SELECT SUM(LENGTH(COALESCE(output,'')) + LENGTH(COALESCE(error,'')) + LENGTH(name)) FROM background_tasks`)
	case "deployments":
		sum = s.scalar(ctx, `SELECT SUM(LENGTH(COALESCE(log,''))) FROM deployments`)
	case "audit_logs":
		sum = s.scalar(ctx, `SELECT SUM(LENGTH(COALESCE(detail,'')) + LENGTH(action) + LENGTH(module)) FROM audit_logs`)
	}
	if sum < 0 {
		sum = 0
	}
	// Row overhead: a page per 8 rows is a rough but stable floor.
	if pageSize <= 0 {
		pageSize = 4096
	}
	return sum + (rows/8+1)*pageSize
}

// scalar runs a query returning one integer; -1 means the query failed.
func (s *Service) scalar(ctx context.Context, query string, args ...any) int64 {
	var value *int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return -1
	}
	if value == nil {
		return 0
	}
	return *value
}

// LargeFile is one file found by the largest-files scan.
type LargeFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// largestFilesRoots are the directories the scan may walk. Anything else is
// refused so the scan cannot be pointed at arbitrary paths.
var largestFilesRoots = []string{"/", "/home", "/var", "/var/log", "/var/lib", "/tmp", "/opt", "/root", "/usr"}

// LargestFiles lists the biggest files under root, largest first. The scan
// stays on one filesystem and ignores virtual ones.
func (s *Service) LargestFiles(ctx context.Context, root string, limit int) ([]LargeFile, error) {
	root = filepath.Clean(root)
	allowed := false
	for _, candidate := range largestFilesRoots {
		if root == candidate {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, model.NewValidationError("scan root must be one of: " + strings.Join(largestFilesRoots, ", "))
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	// -xdev keeps the scan on one filesystem; printing size and path lets
	// the panel sort without walking the tree twice.
	res, err := s.exec.RunSudo(ctx, "find", root, "-xdev", "-type", "f", "-size", "+20M",
		"-printf", "%s\\t%p\\n")
	if err != nil {
		return nil, fmt.Errorf("scan for large files: %w", err)
	}
	if res == nil {
		return []LargeFile{}, nil
	}

	files := make([]LargeFile, 0, limit)
	for _, line := range strings.Split(res.Stdout, "\n") {
		size, path, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		bytes, convErr := strconv.ParseInt(size, 10, 64)
		if convErr != nil || path == "" {
			continue
		}
		files = append(files, LargeFile{Path: path, Bytes: bytes})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Bytes > files[j].Bytes })
	if len(files) > limit {
		files = files[:limit]
	}
	return files, nil
}

// GrowthPoint is one day of recorded usage.
type GrowthPoint struct {
	Day       string `json:"day"`
	UsedBytes int64  `json:"used_bytes"`
}

// GrowthEntry is how much one measured location changed over the window.
type GrowthEntry struct {
	Path       string `json:"path"`
	Label      string `json:"label"`
	DeltaBytes int64  `json:"delta_bytes"`
	Bytes      int64  `json:"bytes"`
}

// GrowthReport compares the newest snapshot with the oldest one in the
// window so the page can say how much was added and by what.
type GrowthReport struct {
	Days          int           `json:"days"`
	UsedBytes     int64         `json:"used_bytes"`
	PreviousBytes int64         `json:"previous_bytes"`
	DeltaBytes    int64         `json:"delta_bytes"`
	Points        []GrowthPoint `json:"points"`
	TopGrowers    []GrowthEntry `json:"top_growers"`
}

// recordUsageSnapshot stores one day's usage; re-recording the same day
// overwrites it, so the history has one row per day.
func (s *Service) recordUsageSnapshot(ctx context.Context, now time.Time, o Overview) error {
	if s.db == nil {
		return nil
	}
	var used, size int64
	for _, fs := range o.Filesystems {
		if fs.MountedOn == "/" {
			used, size = fs.UsedBytes, fs.SizeBytes
			break
		}
	}
	if used == 0 {
		return nil
	}
	dirs := map[string]int64{}
	for _, dir := range o.Dirs {
		dirs[dir.Path] = dir.Bytes
	}
	for _, site := range o.Websites {
		dirs[site.Path] = site.Bytes
	}
	encoded, err := json.Marshal(dirs)
	if err != nil {
		return fmt.Errorf("encode usage snapshot: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO disk_usage_history (day, used_bytes, size_bytes, dirs_json, recorded_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(day) DO UPDATE SET used_bytes = excluded.used_bytes,
			size_bytes = excluded.size_bytes, dirs_json = excluded.dirs_json,
			recorded_at = excluded.recorded_at`,
		now.UTC().Format("2006-01-02"), used, size, string(encoded), now.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("record usage snapshot: %w", err)
	}
	return nil
}

// Growth reports the change over the last days of recorded snapshots.
func (s *Service) Growth(ctx context.Context, now time.Time, days int) (GrowthReport, error) {
	report := GrowthReport{Days: days, Points: []GrowthPoint{}, TopGrowers: []GrowthEntry{}}
	if s.db == nil {
		return report, nil
	}
	if days <= 0 || days > 365 {
		days = 30
	}
	from := now.UTC().AddDate(0, 0, -days).Format("2006-01-02")
	rows, err := s.db.QueryContext(ctx,
		`SELECT day, used_bytes, dirs_json FROM disk_usage_history WHERE day >= ? ORDER BY day`, from)
	if err != nil {
		return report, fmt.Errorf("read usage history: %w", err)
	}
	defer rows.Close()

	var firstDirs, lastDirs map[string]int64
	for rows.Next() {
		var day, dirsJSON string
		var used int64
		if err := rows.Scan(&day, &used, &dirsJSON); err != nil {
			return report, fmt.Errorf("read usage history: %w", err)
		}
		report.Points = append(report.Points, GrowthPoint{Day: day, UsedBytes: used})
		dirs := map[string]int64{}
		_ = json.Unmarshal([]byte(dirsJSON), &dirs)
		if firstDirs == nil {
			firstDirs = dirs
			report.PreviousBytes = used
		}
		lastDirs = dirs
		report.UsedBytes = used
	}
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("read usage history: %w", err)
	}
	if len(report.Points) < 2 {
		// One snapshot cannot show a trend yet.
		report.PreviousBytes = report.UsedBytes
		return report, nil
	}
	report.DeltaBytes = report.UsedBytes - report.PreviousBytes

	labels := map[string]string{}
	for _, p := range analyzedPaths {
		labels[p.Path] = p.Label
	}
	for path, now := range lastDirs {
		delta := now - firstDirs[path]
		if delta <= 0 {
			continue
		}
		report.TopGrowers = append(report.TopGrowers, GrowthEntry{
			Path: path, Label: labels[path], DeltaBytes: delta, Bytes: now,
		})
	}
	sort.SliceStable(report.TopGrowers, func(i, j int) bool {
		return report.TopGrowers[i].DeltaBytes > report.TopGrowers[j].DeltaBytes
	})
	if len(report.TopGrowers) > 10 {
		report.TopGrowers = report.TopGrowers[:10]
	}
	return report, nil
}

// PanelCompactResult reports what compacting the panel database freed.
type PanelCompactResult struct {
	TasksRemoved        int64 `json:"tasks_removed"`
	DeploymentLogsCut   int64 `json:"deployment_logs_cleared"`
	AuditEntriesRemoved int64 `json:"audit_entries_removed"`
	BytesBefore         int64 `json:"bytes_before"`
	BytesAfter          int64 `json:"bytes_after"`
	BytesFreed          int64 `json:"bytes_freed"`
}

// CompactPanelData applies the retention rules now and rebuilds the database
// file, so space held by old task output and audit entries returns to the
// filesystem instead of waiting for the daily pass.
func (s *Service) CompactPanelData(ctx context.Context) (PanelCompactResult, error) {
	var result PanelCompactResult
	if s.db == nil || s.dbPath == "" {
		return result, model.NewValidationError("the panel database is not available")
	}
	report, err := database.ApplyRetention(ctx, s.db, time.Now())
	if err != nil {
		return result, err
	}
	result.TasksRemoved = report.Tasks
	result.DeploymentLogsCut = report.DeploymentLogs
	result.AuditEntriesRemoved = report.AuditEntries

	before, after, err := database.Vacuum(ctx, s.db, s.dbPath)
	if err != nil {
		return result, err
	}
	result.BytesBefore, result.BytesAfter = before, after
	if before > after {
		result.BytesFreed = before - after
	}
	return result, nil
}
