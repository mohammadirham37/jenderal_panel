// Package diskusage reports where disk space goes on the server and runs
// optional cleanup actions. Analysis commands are bounded to a fixed set of
// well-known paths so a scan cannot wander the whole filesystem.
package diskusage

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// Service inspects disk usage and builds cleanup command lists.
type Service struct {
	exec executor.CommandExecutor
}

// NewService creates a new diskusage Service.
func NewService(exec executor.CommandExecutor) *Service {
	return &Service{exec: exec}
}

// analyzedPath pairs a filesystem location with a human label.
type analyzedPath struct {
	Path  string
	Label string
}

// analyzedPaths are the only locations the analyzer looks at. They cover the
// usual growth culprits on a web server while staying predictable.
var analyzedPaths = []analyzedPath{
	{"/var/log", "system_logs"},
	{"/var/cache/apt", "apt_cache"},
	{"/var/www", "websites"},
	{"/var/lib/jenderal/backups", "panel_backups"},
	{"/tmp", "temp_files"},
}

// FilesystemStat is one mounted filesystem from df.
type FilesystemStat struct {
	Source      string `json:"source"`
	SizeBytes   int64  `json:"size_bytes"`
	UsedBytes   int64  `json:"used_bytes"`
	AvailBytes  int64  `json:"avail_bytes"`
	UsePercent  int64  `json:"use_percent"`
	MountedOn   string `json:"mounted_on"`
}

// DirUsage is the measured size of one analyzed path.
type DirUsage struct {
	Path    string `json:"path"`
	Label   string `json:"label"`
	Bytes   int64  `json:"bytes"`
	Missing bool   `json:"missing"`
}

// Overview is the full disk usage report shown on the disk page.
type Overview struct {
	Filesystems  []FilesystemStat `json:"filesystems"`
	Dirs         []DirUsage       `json:"dirs"`
	JournalBytes int64            `json:"journal_bytes"`
	Kernel       string           `json:"kernel"`
	OldKernels   []string         `json:"old_kernels"`
	HasDocker    bool             `json:"has_docker"`
	Docker       []DockerUsage    `json:"docker"`
}

// DockerUsage is one row of `docker system df`.
type DockerUsage struct {
	Type string `json:"type"`
	Size string `json:"size"`
}

// CleanupOptions selects which cleanup actions to run. Zero values skip the
// corresponding action.
type CleanupOptions struct {
	JournalVacuumMB int  `json:"journal_vacuum_mb"`
	AptClean        bool `json:"apt_clean"`
	AptAutoremove   bool `json:"apt_autoremove"`
	TmpCleanDays    int  `json:"tmp_clean_days"`
	DockerPrune     bool `json:"docker_prune"`
}

// CleanupAction describes one planned cleanup step for the dry-run preview.
type CleanupAction struct {
	Name     string   `json:"name"`
	Commands [][]string `json:"commands"`
}

// Overview collects the disk usage report. Missing paths and absent tools
// (docker, journalctl) degrade gracefully: the report still returns with the
// remaining data.
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	overview := Overview{OldKernels: []string{}, Docker: []DockerUsage{}}

	if err := s.collectFilesystems(ctx, &overview); err != nil {
		return overview, err
	}
	s.collectDirSizes(ctx, &overview)
	s.collectJournal(ctx, &overview)
	s.collectKernels(ctx, &overview)
	s.collectDocker(ctx, &overview)

	return overview, nil
}

func (s *Service) collectFilesystems(ctx context.Context, o *Overview) error {
	res, err := s.exec.RunSudo(ctx, "df", "-B1", "-x", "tmpfs", "-x", "devtmpfs", "-x", "squashfs",
		"--output=source,fsize,used,avail,pcent,target")
	if err != nil {
		return fmt.Errorf("df: %w", err)
	}
	o.Filesystems = parseDF(res.Stdout)
	return nil
}

func (s *Service) collectDirSizes(ctx context.Context, o *Overview) {
	paths := make([]string, 0, len(analyzedPaths))
	for _, p := range analyzedPaths {
		paths = append(paths, p.Path)
	}

	args := append([]string{"-sb"}, paths...)
	res, err := s.exec.RunSudo(ctx, "du", args...)
	if err != nil {
		return
	}

	byPath := map[string]int64{}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		bytes, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		byPath[strings.TrimSpace(parts[1])] = bytes
	}

	for _, p := range analyzedPaths {
		usage := DirUsage{Path: p.Path, Label: p.Label}
		if bytes, ok := byPath[p.Path]; ok {
			usage.Bytes = bytes
		} else {
			usage.Missing = true
		}
		o.Dirs = append(o.Dirs, usage)
	}
}

func (s *Service) collectJournal(ctx context.Context, o *Overview) {
	res, err := s.exec.RunSudo(ctx, "journalctl", "--disk-usage")
	if err != nil {
		return
	}
	o.JournalBytes = parseJournalUsage(res.Stdout)
}

func (s *Service) collectKernels(ctx context.Context, o *Overview) {
	res, err := s.exec.Run(ctx, "uname", "-r")
	if err != nil {
		return
	}
	running := strings.TrimSpace(res.Stdout)
	o.Kernel = running

	pkgs, err := s.exec.RunSudo(ctx, "dpkg-query", "-W", "-f=${Package}\n",
		"linux-image-[0-9]*", "linux-headers-[0-9]*", "linux-modules-[0-9]*")
	if err != nil {
		return
	}
	for _, pkg := range strings.Split(strings.TrimSpace(pkgs.Stdout), "\n") {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		// kernel version is the part after the flavor prefix, e.g.
		// linux-image-6.8.0-45-generic -> 6.8.0-45-generic.
		ver := strings.TrimPrefix(pkg, "linux-image-")
		ver = strings.TrimPrefix(strings.TrimPrefix(ver, "linux-headers-"), "linux-modules-")
		if ver != running && !strings.Contains(pkg, running) {
			o.OldKernels = append(o.OldKernels, pkg)
		}
	}
}

func (s *Service) collectDocker(ctx context.Context, o *Overview) {
	res, err := s.exec.RunSudo(ctx, "docker", "system", "df", "--format", "{{.Type}}\t{{.Size}}")
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		o.HasDocker = true
		o.Docker = append(o.Docker, DockerUsage{Type: strings.TrimSpace(parts[0]), Size: strings.TrimSpace(parts[1])})
	}
}

// CleanupPlan returns the actions selected by opts, for the dry-run preview.
func (s *Service) CleanupPlan(opts CleanupOptions) []CleanupAction {
	return cleanupActions(opts)
}

// CleanupCommands returns the argv command list for the selected actions,
// ready for the task runner (which runs every command under sudo).
func (s *Service) CleanupCommands(opts CleanupOptions) [][]string {
	return cleanupCommands(opts)
}

func cleanupActions(opts CleanupOptions) []CleanupAction {
	var actions []CleanupAction
	if opts.JournalVacuumMB > 0 {
		actions = append(actions, CleanupAction{
			Name:     "journal_vacuum",
			Commands: [][]string{{"journalctl", "--vacuum-size=" + strconv.Itoa(opts.JournalVacuumMB) + "M"}},
		})
	}
	if opts.AptClean {
		actions = append(actions, CleanupAction{
			Name: "apt_clean",
			Commands: [][]string{{
				"apt-get", "-o", "DPkg::Lock::Timeout=120", "clean",
			}},
		})
	}
	if opts.AptAutoremove {
		actions = append(actions, CleanupAction{
			Name: "apt_autoremove",
			Commands: [][]string{{
				"apt-get", "-o", "DPkg::Lock::Timeout=120", "autoremove", "-y",
			}},
		})
	}
	if opts.TmpCleanDays > 0 {
		actions = append(actions, CleanupAction{
			Name: "tmp_clean",
			Commands: [][]string{{
				"find", "/tmp", "-xdev", "-mindepth", "1",
				"-mtime", "+" + strconv.Itoa(opts.TmpCleanDays), "-delete",
			}},
		})
	}
	if opts.DockerPrune {
		actions = append(actions, CleanupAction{
			Name: "docker_prune",
			Commands: [][]string{{
				"docker", "system", "prune", "-f",
			}},
		})
	}
	return actions
}

func cleanupCommands(opts CleanupOptions) [][]string {
	var commands [][]string
	for _, action := range cleanupActions(opts) {
		commands = append(commands, action.Commands...)
	}
	return commands
}

// parseDF parses `df --output=...` output, skipping the header line.
func parseDF(out string) []FilesystemStat {
	var stats []FilesystemStat
	for i, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		stat := FilesystemStat{
			Source:    fields[0],
			MountedOn: fields[5],
		}
		stat.SizeBytes, _ = strconv.ParseInt(fields[1], 10, 64)
		stat.UsedBytes, _ = strconv.ParseInt(fields[2], 10, 64)
		stat.AvailBytes, _ = strconv.ParseInt(fields[3], 10, 64)
		stat.UsePercent, _ = strconv.ParseInt(strings.TrimSuffix(fields[4], "%"), 10, 64)
		stats = append(stats, stat)
	}
	return stats
}

// parseJournalUsage extracts the byte size from `journalctl --disk-usage`
// output like "Archived and active journals take up 512.0M ...".
func parseJournalUsage(out string) int64 {
	fields := strings.Fields(out)
	for i, f := range fields {
		if strings.EqualFold(f, "up") && i+1 < len(fields) {
			if size := parseHumanSize(fields[i+1]); size > 0 {
				return size
			}
		}
	}
	return 0
}

// parseHumanSize converts "512.0M", "1.5G", "42.0K" style sizes to bytes.
func parseHumanSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	unit := s[len(s)-1]
	multiplier := int64(1)
	switch unit {
	case 'K', 'k':
		multiplier, s = 1024, s[:len(s)-1]
	case 'M':
		multiplier, s = 1024*1024, s[:len(s)-1]
	case 'G':
		multiplier, s = 1024*1024*1024, s[:len(s)-1]
	case 'T':
		multiplier, s = 1024*1024*1024*1024, s[:len(s)-1]
	}
	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(value * float64(multiplier))
}
