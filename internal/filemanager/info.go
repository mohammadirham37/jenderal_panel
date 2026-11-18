package filemanager

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// FileInfo describes one file or folder in detail. For folders, Size and
// DiskUsage cover everything inside it and Files/Dirs count its contents.
type FileInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // file, directory, other
	Size        int64  `json:"size"`
	DiskUsage   int64  `json:"disk_usage"`
	Files       int64  `json:"files"`
	Dirs        int64  `json:"dirs"`
	Others      int64  `json:"others"`
	Mode        string `json:"mode"`        // octal, e.g. 0755
	Permissions string `json:"permissions"` // symbolic, e.g. drwxr-xr-x
	Owner       string `json:"owner"`
	Group       string `json:"group"`
	Modified    string `json:"modified"`
	Accessed    string `json:"accessed"`
	Changed     string `json:"changed"`
	// Partial is set when some entries inside a folder could not be read,
	// so the totals are a lower bound.
	Partial bool `json:"partial,omitempty"`
}

// sizeTimeout bounds folder walks on very large trees.
const sizeTimeout = 2 * time.Minute

// Info reports details for path, which is relative to the website home.
func (s *Service) Info(ctx context.Context, basePath, subPath string) (FileInfo, error) {
	var info FileInfo
	target, err := s.resolveWebsitePath(ctx, basePath, subPath, false)
	if err != nil {
		return info, err
	}
	ctx, cancel := context.WithTimeout(ctx, sizeTimeout)
	defer cancel()

	res, err := s.runAsWebsiteUser(ctx, basePath, "stat", "-c", "%F\t%s\t%a\t%A\t%U\t%G\t%Y\t%X\t%Z", "--", target)
	if err != nil {
		return info, fmt.Errorf("file info: %w", err)
	}
	if res.ExitCode != 0 {
		return info, model.NewDomainError("FILE_ERROR", "failed to read file info: "+strings.TrimSpace(res.Stderr), nil)
	}
	if err := parseStat(strings.TrimSpace(res.Stdout), &info); err != nil {
		return info, err
	}
	requested, _ := validatePath(basePath, subPath)
	info.Name = filepath.Base(requested)
	info.Path = websiteRelative(basePath, requested)

	diskArgs := []string{"-s", "-B1", "-x", "--", target}
	if info.Type != "directory" {
		diskArgs = []string{"-B1", "--", target}
	}
	if used, ok := s.duBytes(ctx, basePath, diskArgs); ok {
		info.DiskUsage = used
	}
	if info.Type != "directory" {
		return info, nil
	}

	size, sizeOK := s.duBytes(ctx, basePath, []string{"-s", "-b", "-x", "--", target})
	if sizeOK {
		info.Size = size
	}
	// One character per entry: f(ile), d(irectory) or another type.
	res, err = s.runAsWebsiteUser(ctx, basePath, "find", target, "-xdev", "-mindepth", "1", "-printf", "%y")
	if err != nil {
		return info, fmt.Errorf("count folder contents: %w", err)
	}
	for _, c := range res.Stdout {
		switch c {
		case 'f':
			info.Files++
		case 'd':
			info.Dirs++
		default:
			info.Others++
		}
	}
	info.Partial = res.ExitCode != 0 || !sizeOK || ctx.Err() != nil
	return info, nil
}

// DirSizes returns the total size of every folder directly inside path,
// keyed by folder name.
func (s *Service) DirSizes(ctx context.Context, basePath, subPath string) (map[string]int64, error) {
	dir, err := s.resolveWebsitePath(ctx, basePath, subPath, false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, sizeTimeout)
	defer cancel()
	res, err := s.runAsWebsiteUser(ctx, basePath, "du", "-b", "-x", "--max-depth=1", "--", dir)
	if err != nil {
		return nil, fmt.Errorf("folder sizes: %w", err)
	}
	// du exits 1 when some entries are unreadable but still reports the
	// rest.
	if res.ExitCode != 0 && strings.TrimSpace(res.Stdout) == "" {
		return nil, model.NewDomainError("FILE_ERROR", "failed to measure folders: "+strings.TrimSpace(res.Stderr), nil)
	}
	return parseDirSizes(res.Stdout, dir), nil
}

// duBytes runs du and returns the first number it prints.
func (s *Service) duBytes(ctx context.Context, basePath string, args []string) (int64, bool) {
	res, err := s.runAsWebsiteUser(ctx, basePath, "du", args...)
	if err != nil || res == nil {
		return 0, false
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, res.ExitCode == 0
}

func parseStat(line string, info *FileInfo) error {
	parts := strings.Split(line, "\t")
	if len(parts) != 9 {
		return model.NewDomainError("FILE_ERROR", "unexpected file info output", nil)
	}
	switch {
	case parts[0] == "directory":
		info.Type = "directory"
	case strings.HasPrefix(parts[0], "regular"):
		info.Type = "file"
	default:
		info.Type = "other"
	}
	info.Size, _ = strconv.ParseInt(parts[1], 10, 64)
	info.Mode = fmt.Sprintf("%04s", parts[2])
	info.Permissions = parts[3]
	info.Owner = parts[4]
	info.Group = parts[5]
	info.Modified = unixToRFC3339(parts[6])
	info.Accessed = unixToRFC3339(parts[7])
	info.Changed = unixToRFC3339(parts[8])
	return nil
}

func unixToRFC3339(text string) string {
	sec, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// parseDirSizes reads "bytes<TAB>path" lines from du and keeps the direct
// children of dir.
func parseDirSizes(output, dir string) map[string]int64 {
	sizes := map[string]int64{}
	dir = filepath.Clean(dir)
	for _, line := range strings.Split(output, "\n") {
		sizeText, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		size, err := strconv.ParseInt(strings.TrimSpace(sizeText), 10, 64)
		if err != nil {
			continue
		}
		path = filepath.Clean(path)
		if filepath.Dir(path) != dir {
			continue // the folder's own total
		}
		sizes[filepath.Base(path)] = size
	}
	return sizes
}

// websiteRelative turns an absolute path back into the "/..." form the
// file manager uses.
func websiteRelative(basePath, target string) string {
	rel, err := filepath.Rel(filepath.Clean(basePath), target)
	if err != nil || rel == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(rel)
}
