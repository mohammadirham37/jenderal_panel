package website

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// A website's logs live in /home/<user>/logs: nginx writes access.log and
// error.log, PHP-FPM php-error.log and php-slow.log, and logrotate leaves
// numbered or dated archives next to them.

// LogFile is one file in a website's log directory.
type LogFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// logClearTargets are the logs that can be cleared one at a time; "all"
// clears every log in the directory.
var logClearTargets = map[string]bool{"access": true, "error": true, "php-error": true, "php-slow": true, "all": true}

// clearLogsScript empties logs in the website's log directory and prints
// the bytes freed. $1 is the website user, $2 the target.
//
// The directory belongs to the website user, so it must not be able to
// point root at another file: the script enters the directory with cd -P
// and checks it really is /home/<user>/logs, then works on relative names
// only, skips symlinks, and truncates with O_NOFOLLOW. Active logs are
// truncated rather than deleted because nginx and PHP-FPM keep them open
// (in append mode, so writing continues at the new end). Rotated archives
// are deleted.
const clearLogsScript = `set -u
dir="/home/$1/logs"
cd -P -- "$dir" 2>/dev/null || { echo 0; exit 0; }
[ "$(pwd -P)" = "$dir" ] || { echo "the logs directory is not a plain directory" >&2; exit 3; }
freed=0
regular() { [ -f "$1" ] && [ ! -L "$1" ]; }
empty_log() {
	regular "$1" || return 0
	size=$(wc -c < "$1")
	dd if=/dev/null of="$1" oflag=nofollow status=none && freed=$((freed + size))
}
remove_archive() {
	regular "$1" || return 0
	size=$(wc -c < "$1")
	rm -f -- "$1" && freed=$((freed + size))
}
if [ "$2" = all ]; then
	for f in *.log; do empty_log "$f"; done
	for f in *.log.[0-9]* *.log-[0-9]*; do remove_archive "$f"; done
else
	empty_log "$2.log"
	for f in "$2".log.[0-9]* "$2".log-[0-9]*; do remove_archive "$f"; done
fi
echo "$freed"
`

// ClearLogs empties one of the website's logs (and removes its rotated
// archives), or every log when target is "all". It returns the bytes freed.
func (s *Service) ClearLogs(ctx context.Context, id, target string) (int64, error) {
	if !logClearTargets[target] {
		return 0, model.NewValidationError("target must be access, error, php-error, php-slow or all")
	}
	w, err := s.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	if !goCacheUserPattern.MatchString(w.WebUser) {
		return 0, model.NewValidationError("invalid website user")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := s.exec.RunSudo(ctx, "/bin/bash", "-c", clearLogsScript, "clear-logs", w.WebUser, target)
	if err != nil {
		return 0, fmt.Errorf("clear logs: %w", err)
	}
	if res == nil || res.ExitCode != 0 {
		detail := "no result"
		if res != nil {
			detail = strings.TrimSpace(res.Stderr)
		}
		return 0, fmt.Errorf("clear logs: %s", detail)
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) == 0 {
		return 0, nil
	}
	freed, _ := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	return freed, nil
}

// LogFiles lists the regular files in the website's log directory with
// their sizes, largest first.
func (s *Service) LogFiles(ctx context.Context, id string) ([]LogFile, error) {
	files := []LogFile{}
	w, err := s.Get(ctx, id)
	if err != nil {
		return files, err
	}
	if !goCacheUserPattern.MatchString(w.WebUser) {
		return files, model.NewValidationError("invalid website user")
	}
	res, err := s.exec.RunSudo(ctx, "find", "/home/"+w.WebUser+"/logs/", "-maxdepth", "1", "-type", "f", "-printf", `%f\t%s\n`)
	if err != nil {
		return files, fmt.Errorf("list logs: %w", err)
	}
	if res == nil || res.ExitCode != 0 {
		return files, nil // no log directory yet
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		name, sizeText, ok := strings.Cut(line, "\t")
		if !ok || name == "" {
			continue
		}
		size, err := strconv.ParseInt(strings.TrimSpace(sizeText), 10, 64)
		if err != nil {
			continue
		}
		files = append(files, LogFile{Name: name, Size: size})
	}
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Size != files[j].Size {
			return files[i].Size > files[j].Size
		}
		return files[i].Name < files[j].Name
	})
	return files, nil
}
