// Package phpcli makes a website's command-line PHP match the PHP version
// the website runs on. Ubuntu's /usr/bin/php is one server-wide default
// (update-alternatives), so without this a site on PHP 8.2 ran composer,
// artisan, and its cron jobs with whatever version that default was.
//
// Each PHP website gets ~/.jenderal/bin/php, a symlink to
// /usr/bin/php<version>. Panel commands and cron jobs put that directory
// first on PATH, and the site's login shells (SSH, web terminal) do too,
// so "php" and "#!/usr/bin/env php" scripts such as composer resolve to the
// site's version.
package phpcli

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

var (
	versionPattern = regexp.MustCompile(`^[0-9]\.[0-9]$`)
	userPattern    = regexp.MustCompile(`^web_[a-z0-9_]{1,40}$`)
)

// profileMarker identifies the PATH line added to the site's shell profiles.
const profileMarker = "# jenderal: site PHP version first on PATH"

// ShimDir is the directory holding the website's php symlink.
func ShimDir(webUser string) string { return "/home/" + webUser + "/.jenderal/bin" }

// PathPrefix returns the PATH assignment that puts the site's PHP first,
// for shell snippets and crontabs.
func PathPrefix(webUser string) string { return ShimDir(webUser) + ":" }

// Binary is the version-specific PHP binary, e.g. /usr/bin/php8.2.
func Binary(version string) string { return "/usr/bin/php" + version }

// EnsureSiteShim points ~/.jenderal/bin/php at the site's PHP version and
// makes the site's login shells pick it up. It runs as the website user.
func EnsureSiteShim(ctx context.Context, exec executor.CommandExecutor, webUser, version string) error {
	if !userPattern.MatchString(webUser) {
		return fmt.Errorf("invalid website user %q", webUser)
	}
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid PHP version %q", version)
	}
	dir := ShimDir(webUser)
	line := `[ -d "$HOME/.jenderal/bin" ] && PATH="$HOME/.jenderal/bin:$PATH" ` + profileMarker
	script := `set -e
mkdir -p "$1"
ln -sfn "$2" "$1/php"
for f in "$HOME/.profile" "$HOME/.bashrc"; do
  [ -L "$f" ] && continue
  grep -qF "$4" "$f" 2>/dev/null || printf '\n%s\n' "$3" >> "$f"
done`
	res, err := exec.RunSudo(ctx, "-u", webUser, "-H", "--", "/bin/bash", "-c", script, "--",
		dir, Binary(version), line, profileMarker)
	if err != nil {
		return fmt.Errorf("set site PHP CLI: %w", err)
	}
	if res == nil || res.ExitCode != 0 {
		detail := ""
		if res != nil {
			detail = strings.TrimSpace(res.Stderr)
		}
		return fmt.Errorf("set site PHP CLI: %s", detail)
	}
	return nil
}

// QualifyCommand rewrites a command that starts with a bare "php" to the
// site's version-specific binary, for places that do not search PATH
// (systemd ExecStart).
func QualifyCommand(command, version string) string {
	if !versionPattern.MatchString(version) {
		return command
	}
	trimmed := strings.TrimLeft(command, " ")
	if trimmed == "php" || strings.HasPrefix(trimmed, "php ") {
		return Binary(version) + strings.TrimPrefix(trimmed, "php")
	}
	return command
}
