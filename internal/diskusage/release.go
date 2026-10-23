package diskusage

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Large files found by the scan are often logs. Only files under /var/log
// can be released from the panel, and only in the way that is safe for each
// kind: a log a program still writes to is emptied in place (deleting it
// would leave the program writing to an unlinked file that keeps the space),
// while a rotated archive nobody writes to is deleted.

// Release actions for a large file.
const (
	ReleaseTruncate = "truncate"
	ReleaseDelete   = "delete"
)

const releaseRoot = "/var/log/"

// rotatedLogPattern matches archives written by logrotate: syslog.1,
// auth.log.2.gz, access.log-20260101, error.log.old and similar.
var rotatedLogPattern = regexp.MustCompile(`(\.\d+(\.gz|\.xz|\.bz2|\.zst)?|\.(gz|xz|bz2|zst)|-\d{8}(\.gz)?|\.old)$`)

// loginRecordLogs are the binary login records. They are written by login
// and sshd, and Ubuntu rotates them only monthly, so a brute-force attack
// can grow btmp to gigabytes.
var loginRecordLogs = map[string]string{
	"/var/log/btmp": "btmp",
	"/var/log/wtmp": "wtmp",
}

// releaseActionFor says how a file may be released from the panel, or ""
// when it may not be touched.
func releaseActionFor(path string) string {
	clean := filepath.Clean(path)
	if clean != path || !strings.HasPrefix(clean, releaseRoot) {
		return ""
	}
	// The systemd journal has its own size controls (journalctl --vacuum).
	if strings.HasPrefix(clean, releaseRoot+"journal/") {
		return ""
	}
	// lastlog is a sparse file: its apparent size is not disk usage.
	if clean == "/var/log/lastlog" {
		return ""
	}
	if _, ok := loginRecordLogs[clean]; ok {
		return ReleaseTruncate
	}
	if rotatedLogPattern.MatchString(filepath.Base(clean)) {
		return ReleaseDelete
	}
	return ReleaseTruncate
}

// loginRecordRotation caps btmp/wtmp by size on top of the monthly rotation,
// so they are rotated by the daily logrotate run once they pass 50 MB.
func loginRecordRotation(name string) string {
	return fmt.Sprintf(`# Managed by Jenderal Panel: rotate monthly, or sooner once over 50 MB.
/var/log/%s {
    missingok
    monthly
    maxsize 50M
    create %s root utmp
    rotate 1
}
`, name, map[string]string{"btmp": "0660", "wtmp": "0664"}[name])
}

// ReleaseResult reports what releasing a file did.
type ReleaseResult struct {
	Path           string `json:"path"`
	Action         string `json:"action"`
	BytesFreed     int64  `json:"bytes_freed"`
	RotationCapped bool   `json:"rotation_capped"`
}

// ReleaseFile empties or deletes one large log file, as releaseActionFor
// allows. For btmp and wtmp it also installs a size-based logrotate rule so
// the file does not fill the disk again.
func (s *Service) ReleaseFile(ctx context.Context, path string) (ReleaseResult, error) {
	result := ReleaseResult{Path: path}
	action := releaseActionFor(path)
	if action == "" {
		return result, model.NewValidationError("only log files under /var/log can be released")
	}
	result.Action = action

	// The resolved path must be the path itself: a symlink anywhere along it
	// could point the action at a file outside /var/log.
	res, err := s.exec.RunSudo(ctx, "realpath", "-e", "--", path)
	if err != nil {
		return result, fmt.Errorf("resolve %s: %w", path, err)
	}
	if res == nil || res.ExitCode != 0 {
		return result, model.NewDomainError("NOT_FOUND", "file not found", nil)
	}
	if strings.TrimSpace(res.Stdout) != path {
		return result, model.NewValidationError("the path resolves through a symbolic link")
	}
	res, err = s.exec.RunSudo(ctx, "stat", "-c", "%F\t%s", "--", path)
	if err != nil {
		return result, fmt.Errorf("inspect %s: %w", path, err)
	}
	if res == nil || res.ExitCode != 0 {
		return result, model.NewDomainError("NOT_FOUND", "file not found", nil)
	}
	kind, size, _ := strings.Cut(strings.TrimSpace(res.Stdout), "\t")
	if kind != "regular file" && kind != "regular empty file" {
		return result, model.NewValidationError("only regular files can be released")
	}
	fmt.Sscan(size, &result.BytesFreed)

	var cmd []string
	if action == ReleaseTruncate {
		cmd = []string{"truncate", "-s", "0", "--", path}
	} else {
		cmd = []string{"rm", "-f", "--", path}
	}
	res, err = s.exec.RunSudo(ctx, cmd[0], cmd[1:]...)
	if err != nil {
		return result, fmt.Errorf("release %s: %w", path, err)
	}
	if res == nil || res.ExitCode != 0 {
		stderr := ""
		if res != nil {
			stderr = strings.TrimSpace(res.Stderr)
		}
		return result, fmt.Errorf("release %s: %s", path, stderr)
	}

	if name, ok := loginRecordLogs[path]; ok {
		res, err = s.exec.RunSudoWithInput(ctx, loginRecordRotation(name), "tee", "/etc/logrotate.d/"+name)
		result.RotationCapped = err == nil && res != nil && res.ExitCode == 0
	}
	return result, nil
}
