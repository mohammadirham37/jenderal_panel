package noderuntime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Every Node install and security update adds a version under
// ~/.nvm/versions/node next to the old one, and nvm keeps the downloaded
// tarballs in ~/.nvm/.cache. Neither was ever removed, so each website kept
// several 200 MB runtimes it no longer used.

const (
	pruneTimeout        = 5 * time.Minute
	pruneCommandTimeout = "4m"
)

// PruneResult reports what pruning one website's runtimes removed.
type PruneResult struct {
	Kept       string   `json:"kept"`
	Removed    []string `json:"removed"`
	BytesFreed int64    `json:"bytes_freed"`
}

// Prune removes every installed Node version except the newest one of the
// selected major (the one nvm-exec resolves NODE_VERSION to) and any
// version a project in the website home pins with .nvmrc, then clears the
// nvm download cache. It runs as the website user inside its own home.
func (s *Service) Prune(ctx context.Context, user, version string, log func(string)) (PruneResult, error) {
	var result PruneResult
	args, err := runtimeShellArgs(user, version, pruneCommandTimeout, pruneScript)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, pruneTimeout)
	defer cancel()
	res, err := s.exec.RunSudo(ctx, "-u", args...)
	if err != nil {
		return result, fmt.Errorf("prune Node runtimes: %w", err)
	}
	if res == nil {
		return result, errors.New("prune Node runtimes: executor returned no result")
	}
	if res.ExitCode != 0 {
		return result, commandError("prune Node runtimes", res)
	}
	return parsePruneOutput(res.Stdout, log), nil
}

func parsePruneOutput(output string, log func(string)) PruneResult {
	result := PruneResult{Removed: []string{}}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok {
			continue
		}
		switch key {
		case "kept":
			result.Kept = value
		case "removed":
			name, size, _ := strings.Cut(value, " ")
			bytes, _ := strconv.ParseInt(size, 10, 64)
			result.Removed = append(result.Removed, name)
			result.BytesFreed += bytes
			if log != nil {
				log(fmt.Sprintf("Removed unused Node.js %s (%d MB)", name, bytes>>20))
			}
		case "cache":
			bytes, _ := strconv.ParseInt(value, 10, 64)
			result.BytesFreed += bytes
			if log != nil && bytes > 0 {
				log(fmt.Sprintf("Cleared the nvm download cache (%d MB)", bytes>>20))
			}
		}
	}
	return result
}

const pruneScript = `set -eu
user=$1
version=$2
expected_home=$3
expected_commit=` + nvmCommit + `

if [ "$HOME" != "$expected_home" ] || [ "$NVM_DIR" != "$expected_home/.nvm" ]; then echo 'invalid runtime home' >&2; exit 64; fi
if [ ! -e "$NVM_DIR" ]; then exit 0; fi
for path in "$HOME" "$NVM_DIR" "$NVM_DIR/versions" "$NVM_DIR/versions/node"; do
  if [ -L "$path" ]; then echo "Refusing symlinked runtime path: $path" >&2; exit 65; fi
done
actual_commit=''
if [ -f "$NVM_DIR/.jenderal-nvm-commit" ]; then IFS= read -r actual_commit < "$NVM_DIR/.jenderal-nvm-commit" || true; fi
if [ "$actual_commit" != "$expected_commit" ] || [ ! -f "$NVM_DIR/nvm.sh" ]; then echo 'NVM installation has unexpected identity; not pruning' >&2; exit 69; fi
. "$NVM_DIR/nvm.sh"

current=$(nvm version "$version" 2>/dev/null || true)
case "$current" in v"$version".*) ;; *) echo "Node $version is not installed; not pruning" >&2; exit 70;; esac
printf 'kept=%s\n' "$current"
keep=" $current "

# Projects that pin a version with .nvmrc keep the version it resolves to.
find "$HOME" -maxdepth 4 -name .nvmrc -type f -not -path "$NVM_DIR/*" -not -path '*/node_modules/*' 2>/dev/null |
  while IFS= read -r rc; do
    spec=$(head -n 1 -- "$rc" | tr -d '[:space:]')
    [ -n "$spec" ] || continue
    pinned=$(nvm version "$spec" 2>/dev/null || true)
    case "$pinned" in v[0-9]*) printf '%s\n' "$pinned";; esac
  done > "$NVM_DIR/.jenderal-pinned" || true
while IFS= read -r pinned; do keep="$keep$pinned "; done < "$NVM_DIR/.jenderal-pinned"
rm -f -- "$NVM_DIR/.jenderal-pinned"

if [ -d "$NVM_DIR/versions/node" ]; then
  for dir in "$NVM_DIR"/versions/node/v*; do
    [ -d "$dir" ] && [ ! -L "$dir" ] || continue
    name=${dir##*/}
    case "$keep" in *" $name "*) continue;; esac
    size=$(du -sb -- "$dir" | cut -f1)
    rm -rf -- "$dir"
    printf 'removed=%s %s\n' "$name" "$size"
  done
fi

if [ -d "$NVM_DIR/.cache" ] && [ ! -L "$NVM_DIR/.cache" ]; then
  size=$(du -sb -- "$NVM_DIR/.cache" | cut -f1)
  nvm cache clear >/dev/null 2>&1 || true
  after=0
  if [ -d "$NVM_DIR/.cache" ]; then after=$(du -sb -- "$NVM_DIR/.cache" | cut -f1); fi
  printf 'cache=%s\n' "$((size - after))"
fi
`
