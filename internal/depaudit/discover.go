package depaudit

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// Site is a website whose lockfiles are scanned.
type Site struct {
	ID, Domain, WebUser, DocumentRoot string
}

// maxLockfileBytes bounds a lockfile read into memory.
const maxLockfileBytes = 20 << 20

var webUserPattern = regexp.MustCompile(`^web_[a-z0-9_]+$`)

// projectDirs lists where a site's lockfiles may live, most specific first:
// the Laravel project (<home>/app) when the document root is its public
// directory, the document root, then the site home.
func projectDirs(s Site) []string {
	home := "/home/" + s.WebUser
	var dirs []string
	seen := map[string]bool{}
	addDir := func(d string) {
		d = filepath.Clean(d)
		if d != "" && !seen[d] && (d == home || strings.HasPrefix(d, home+"/")) {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if filepath.Clean(s.DocumentRoot) == filepath.Join(home, "app", "public") {
		addDir(filepath.Join(home, "app"))
	}
	if s.DocumentRoot != "" {
		addDir(s.DocumentRoot)
	}
	addDir(home)
	return dirs
}

// siteDependencies reads each lockfile kind from the first project
// directory that has it. Unreadable, oversize or unparsable lockfiles are
// skipped. It returns the dependencies and the lockfile paths used.
func siteDependencies(ctx context.Context, exec executor.CommandExecutor, s Site) ([]Dependency, []string, error) {
	if !webUserPattern.MatchString(s.WebUser) {
		return nil, nil, fmt.Errorf("unsafe website user %q", s.WebUser)
	}
	var deps []Dependency
	var read []string
	for _, name := range Lockfiles {
		for _, dir := range projectDirs(s) {
			path := filepath.Join(dir, name)
			if res, err := exec.RunSudo(ctx, "-u", s.WebUser, "--", "test", "-f", path); err != nil || res == nil || res.ExitCode != 0 {
				continue
			}
			data, ok := readCapped(ctx, exec, s.WebUser, path)
			if ok {
				if parsed, err := ParseLockfile(name, data); err == nil {
					deps = append(deps, parsed...)
					read = append(read, path)
				}
			}
			break // the first directory holding this lockfile wins
		}
	}
	return deps, read, nil
}

// readCapped reads path as the site account, so a symlink planted in the
// site cannot make the panel read files the site itself cannot.
func readCapped(ctx context.Context, exec executor.CommandExecutor, user, path string) ([]byte, bool) {
	res, err := exec.RunSudo(ctx, "-u", user, "--", "stat", "-L", "-c", "%s", path)
	if err != nil || res == nil || res.ExitCode != 0 {
		return nil, false
	}
	size, err := strconv.ParseInt(strings.TrimSpace(res.Stdout), 10, 64)
	if err != nil || size > maxLockfileBytes {
		return nil, false
	}
	res, err = exec.RunSudo(ctx, "-u", user, "--", "cat", path)
	if err != nil || res == nil || res.ExitCode != 0 || len(res.Stdout) > maxLockfileBytes {
		return nil, false
	}
	return []byte(res.Stdout), true
}
