package noderuntime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	removeTimeout        = 5 * time.Minute
	removeCommandTimeout = "4m"
)

// Remove deletes a website's NVM, every Node version it installed, and the
// npm cache, returning the bytes freed. It runs as the website user inside
// its own home and refuses symlinked runtime paths.
func (s *Service) Remove(ctx context.Context, user string) (int64, error) {
	home, err := Home(user)
	if err != nil {
		return 0, err
	}
	args := []string{
		user, "--", "/usr/bin/env", "-i",
		"HOME=" + home, "USER=" + user, "LOGNAME=" + user,
		"NVM_DIR=" + home + "/.nvm",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"/usr/bin/timeout", "--signal=TERM", "--kill-after=5s", removeCommandTimeout,
		"/bin/bash", "-c", removeScript, "--", home,
	}
	ctx, cancel := context.WithTimeout(ctx, removeTimeout)
	defer cancel()
	res, err := s.exec.RunSudo(ctx, "-u", args...)
	if err != nil {
		return 0, fmt.Errorf("remove Node runtime: %w", err)
	}
	if res == nil {
		return 0, errors.New("remove Node runtime: executor returned no result")
	}
	if res.ExitCode != 0 {
		return 0, commandError("remove Node runtime", res)
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "freed="); ok {
			freed, _ := strconv.ParseInt(value, 10, 64)
			return freed, nil
		}
	}
	return 0, nil
}

// removeScript deletes ~/.nvm (with any interrupted install stage) and the
// npm cache. Paths are checked before removal so a symlink planted in the
// home cannot redirect the delete outside it.
const removeScript = `set -eu
expected_home=$1
if [ "$HOME" != "$expected_home" ] || [ "$NVM_DIR" != "$expected_home/.nvm" ]; then echo 'invalid runtime home' >&2; exit 64; fi
if [ -L "$HOME" ]; then echo 'Refusing symlinked home' >&2; exit 65; fi
freed=0
for path in "$NVM_DIR" "$HOME"/.nvm-stage.* "$HOME"/.nvm-incomplete.*.bak "$HOME/.npm"; do
  [ -e "$path" ] || [ -L "$path" ] || continue
  if [ -L "$path" ]; then rm -f -- "$path"; continue; fi
  size=$(du -sb -- "$path" | cut -f1)
  rm -rf -- "$path"
  freed=$((freed + size))
done
printf 'freed=%s\n' "$freed"
`
