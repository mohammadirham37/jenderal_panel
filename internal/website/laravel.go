package website

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var laravelEnvLine = regexp.MustCompile(`^\s*(?:export\s+)?(APP_KEY|DB_CONNECTION|DB_DATABASE|DB_URL|DATABASE_URL|DB_HOST|DB_PORT|DB_USERNAME|DB_PASSWORD)\s*=\s*(.*?)\s*$`)
var laravelAppKeyLine = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?APP_KEY[ \t]*=.*$`)

// laravelEnvironment changes only SQLite settings, retaining unrelated dotenv
// content and app keys. Existing external connections are never migrated.
func laravelEnvironment(content, root string, fresh bool) (string, string, bool, error) {
	lines := strings.Split(content, "\n")
	values := map[string]string{}
	indexes := map[string]int{}
	for n, line := range lines {
		m := laravelEnvLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if _, ok := indexes[m[1]]; ok {
			return "", "", false, fmt.Errorf("duplicate %s in Laravel .env; resolve it before repair", m[1])
		}
		raw := strings.TrimSpace(m[2])
		value := raw
		if strings.HasPrefix(raw, "#") {
			value = ""
		}
		if strings.HasPrefix(raw, "\"") || strings.HasPrefix(raw, "'") {
			quote := raw[0]
			end := -1
			for pos := 1; pos < len(raw); pos++ {
				if raw[pos] == '\\' && quote == '"' {
					pos++
					continue
				}
				if raw[pos] == quote {
					end = pos
					break
				}
			}
			if end < 0 {
				return "", "", false, fmt.Errorf("invalid quoted %s", m[1])
			}
			suffix := strings.TrimSpace(raw[end+1:])
			if suffix != "" && !strings.HasPrefix(suffix, "#") {
				return "", "", false, fmt.Errorf("invalid %s value", m[1])
			}
			value = raw[1:end]
			if quote == '"' {
				decoded, err := strconv.Unquote(raw[:end+1])
				if err != nil {
					return "", "", false, fmt.Errorf("invalid %s quoting", m[1])
				}
				value = decoded
			}
		} else if pos := strings.Index(raw, " #"); pos >= 0 {
			value = strings.TrimSpace(raw[:pos])
		}
		values[m[1]] = value
		indexes[m[1]] = n
	}
	connection := values["DB_CONNECTION"]
	if strings.ContainsAny(connection, "${}") {
		return "", "", false, fmt.Errorf("resolve DB_CONNECTION before Laravel repair")
	}
	if !fresh && connection == "" {
		for _, key := range []string{"DB_DATABASE", "DB_HOST", "DB_PORT", "DB_USERNAME", "DB_PASSWORD"} {
			if values[key] != "" {
				return content, "", false, nil
			}
		}
	}
	if !fresh && ((connection != "" && connection != "sqlite") || values["DB_URL"] != "" || values["DATABASE_URL"] != "") {
		return content, "", false, nil
	}
	database := values["DB_DATABASE"]
	if fresh || database == "" {
		database = filepath.Join(root, "database", "database.sqlite")
	}
	if strings.ContainsAny(database, "${}\r\n") || database == ":memory:" {
		return "", "", false, fmt.Errorf("SQLite database must be a persistent path within the project")
	}
	if !filepath.IsAbs(database) {
		database = filepath.Join(root, database)
	}
	database = filepath.Clean(database)
	if !strings.HasPrefix(database, filepath.Clean(root)+string(filepath.Separator)) {
		return "", "", false, fmt.Errorf("SQLite database must remain inside the Laravel project")
	}
	set := func(key, value string) {
		line := key + "=" + value
		if n, ok := indexes[key]; ok {
			lines[n] = line
		} else {
			lines = append(lines, line)
		}
	}
	set("DB_CONNECTION", "sqlite")
	set("DB_DATABASE", strconv.Quote(database))
	if _, exists := indexes["APP_KEY"]; !exists {
		set("APP_KEY", "")
	}
	if fresh {
		for _, key := range []string{"DB_URL", "DATABASE_URL"} {
			if _, ok := indexes[key]; ok {
				set(key, "")
			}
		}
	}
	return strings.Join(lines, "\n"), database, values["APP_KEY"] == "", nil
}

func (i *Installer) bootstrapLaravel(ctx context.Context, w websiteRow, root string, fresh bool, progress func(string, string) error) error {
	profile, err := profileForWebsiteRow(w)
	if err != nil {
		return err
	}
	if profile.SetupMode != SetupAutomatic || profile.Framework != "laravel" {
		return nil
	}
	final, _, err := installerFinalPaths(w)
	if err != nil {
		return err
	}
	if root != final {
		return fmt.Errorf("Laravel bootstrap requires final project path")
	}
	if _, err := installerStagingRoot(w); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := progress("initializing Laravel", "Checking Laravel environment and SQLite configuration.\n"); err != nil {
		return err
	}
	// All filesystem access runs as the tenant. Refuse symlinked project/env
	// paths before reading or writing environment data.
	check := `set -eu; root=$1; [ ! -L "$root" ] && [ ! -L "$root/.env" ] || exit 1; if [ ! -f "$root/.env" ]; then umask 077; cp -- "$root/.env.example" "$root/.env"; fi; cat -- "$root/.env"`
	result, err := i.exec.RunSudo(ctx, "-u", w.WebUser, "--", "/bin/bash", "-c", check, "--", root)
	if err != nil {
		return fmt.Errorf("read Laravel environment: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("cannot read Laravel environment; check project ownership and symlinks")
	}
	updated, database, needsKey, err := laravelEnvironment(result.Stdout, root, fresh)
	if err != nil {
		return err
	}
	if database == "" {
		return progress("initializing Laravel", "External database configuration preserved; database migrations remain manual.\n")
	}
	php := "/usr/bin/php" + w.PHPVersion
	run := func(label, command string, args ...string) error {
		if err := progress("initializing Laravel", "\n=== "+label+" ===\n"); err != nil {
			return err
		}
		prefix := []string{w.WebUser, "--", "/usr/bin/env", "HOME=/home/" + w.WebUser, command}
		r, e := i.exec.RunSudo(ctx, "-u", append(prefix, args...)...)
		if r != nil {
			if err := progress("initializing Laravel", r.Stdout+r.Stderr); err != nil {
				return err
			}
		}
		if e != nil {
			return fmt.Errorf("%s: %w", label, e)
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("%s failed (exit %d); see provisioning log", label, r.ExitCode)
		}
		return nil
	}
	if needsKey {
		if err := progress("initializing Laravel", "Generating missing Laravel app key.\n"); err != nil {
			return err
		}
		keyResult, keyErr := i.exec.RunSudo(ctx, "-u", w.WebUser, "--", "/usr/bin/env", "HOME=/home/"+w.WebUser, php, root+"/artisan", "key:generate", "--show", "--no-ansi", "--no-interaction")
		if keyErr != nil {
			return fmt.Errorf("generate Laravel key: %w", keyErr)
		}
		if keyResult.ExitCode != 0 {
			return fmt.Errorf("generate Laravel key failed; check Laravel bootstrap and dependencies")
		}
		key := strings.TrimSpace(keyResult.Stdout)
		decoded, decodeErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(key, "base64:"))
		if !strings.HasPrefix(key, "base64:") || decodeErr != nil || (len(decoded) != 16 && len(decoded) != 32) {
			return fmt.Errorf("Laravel returned an invalid app key")
		}
		updated = laravelAppKeyLine.ReplaceAllString(updated, "APP_KEY="+key)
	}
	if updated != result.Stdout {
		write := `set -eu; umask 077; [ ! -L "$1" ]; staging=$(mktemp "$1.jenderal.XXXXXX"); trap 'rm -f -- "$staging"' EXIT; cat > "$staging"; mv -f -- "$staging" "$1"`
		r, e := i.exec.RunSudoWithInput(ctx, updated, "-u", w.WebUser, "--", "/bin/bash", "-c", write, "--", root+"/.env")
		if e != nil {
			return fmt.Errorf("save Laravel environment: %w", e)
		}
		if r.ExitCode != 0 {
			return fmt.Errorf("save Laravel environment failed")
		}
	}
	if err := run("check PHP SQLite extension", php, "-r", `if (!extension_loaded('pdo_sqlite')) { fwrite(STDERR, "Missing pdo_sqlite: install the selected PHP version's sqlite3 extension from PHP management, then retry.\n"); exit(1); }`); err != nil {
		return err
	}
	prepare := `set -eu; root=$1; db=$2
 for target in "$root/database" "$root/storage" "$root/bootstrap" "$root/bootstrap/cache" "$db"; do
   part=$target
   while [ "$part" != "$root" ]; do [ ! -L "$part" ] || { echo "Refusing symlinked Laravel writable path" >&2; exit 1; }; part=$(dirname "$part"); done
 done
 umask 027
 chmod u+rwx "$root"
 for writable in "$root/database" "$root/storage" "$root/bootstrap/cache"; do
   if [ -d "$writable" ]; then
     chmod u+rwx,go-w "$writable"
     find "$writable" -type d -exec chmod u+rwx,go-w {} \;
     find "$writable" -type f -exec chmod u+rw,go-w {} \;
   fi
 done
 for target in "$(dirname "$db")" "$root/bootstrap"; do
   while [ "$target" != "$root" ]; do if [ -d "$target" ]; then chmod u+rwx "$target"; fi; target=$(dirname "$target"); done
 done
 mkdir -p -- "$(dirname "$db")" "$root/database" "$root/storage/framework/cache/data" "$root/storage/framework/sessions" "$root/storage/framework/views" "$root/storage/logs" "$root/bootstrap/cache"
 if [ ! -e "$db" ]; then touch -- "$db"; fi
 [ -f "$db" ] || exit 1
 chmod u+rw "$db"
 chmod u+rwx "$(dirname "$db")" "$root/database" "$root/storage" "$root/bootstrap/cache"
 `
	if err := run("prepare Laravel writable paths", "/bin/bash", "-c", prepare, "--", root, database); err != nil {
		return err
	}
	if err := run("clear stale Laravel configuration", php, root+"/artisan", "config:clear", "--no-interaction"); err != nil {
		return err
	}
	return run("migrate Laravel SQLite", php, root+"/artisan", "migrate", "--force", "--no-interaction")
}
