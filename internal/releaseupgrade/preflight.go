package releaseupgrade

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Check statuses.
const (
	CheckOK    = "ok"
	CheckWarn  = "warn"
	CheckBlock = "block"
)

// Check is one preflight result.
type Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Preflight is the full readiness report. Blocked is true when any check
// blocks the upgrade.
type Preflight struct {
	Current string  `json:"current"`
	Target  string  `json:"target"`
	Checks  []Check `json:"checks"`
	Blocked bool    `json:"blocked"`
	// PostgreSQL is set when clusters older than ToPostgresMajor exist.
	PostgreSQL *PostgresMigration `json:"postgresql,omitempty"`

	// mysqlNative is passed to the script (re-enable the plugin).
	mysqlNative bool
	// phpVersions are the versions sites use.
	phpVersions []string
}

const (
	minRootFree = 5 << 30   // 5 GiB
	minBootFree = 300 << 20 // 300 MiB
)

func (p *Preflight) add(id, status, message string) {
	p.Checks = append(p.Checks, Check{ID: id, Status: status, Message: message})
	if status == CheckBlock {
		p.Blocked = true
	}
}

// Preflight runs every check. On anything but noble only the release check
// is reported, since nothing else applies.
func (s *Service) Preflight(ctx context.Context) (*Preflight, error) {
	p := &Preflight{Target: ToVersion, Checks: []Check{}}
	codename := s.codename(ctx)
	p.Current = codename
	switch codename {
	case FromCodename:
		p.add("release", CheckOK, "Ubuntu "+FromVersion+" (noble)")
	case ToCodename:
		p.add("release", CheckBlock, "This server already runs Ubuntu "+ToVersion+".")
		return p, nil
	default:
		p.add("release", CheckBlock, fmt.Sprintf("Only Ubuntu %s can be upgraded to %s from the panel (this server reports %q).", FromVersion, ToVersion, codename))
		return p, nil
	}

	if st, _ := s.Status(ctx); st != nil && st.Running {
		p.add("panel_task", CheckBlock, "A release upgrade is already running.")
	}
	s.checkAvailable(ctx, p)
	s.checkPendingUpdates(ctx, p)
	s.checkRebootRequired(ctx, p)
	s.checkDpkg(ctx, p)
	s.checkDisk(ctx, p)
	s.checkSources(ctx, p)
	s.checkPHP(ctx, p)
	s.checkMySQL(ctx, p)
	s.checkPostgreSQL(ctx, p)
	return p, nil
}

func (s *Service) codename(ctx context.Context) string {
	res, err := s.exec.Run(ctx, "/bin/cat", "/etc/os-release")
	if err != nil || res == nil || res.ExitCode != 0 {
		return ""
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "VERSION_CODENAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// checkAvailable needs the upgrader installed and Canonical's LTS
// meta-release to offer resolute as supported (it is only offered to LTS
// users from the first point release).
func (s *Service) checkAvailable(ctx context.Context, p *Preflight) {
	if res, err := s.exec.Run(ctx, "/usr/bin/test", "-x", "/usr/bin/do-release-upgrade"); err != nil || res == nil || res.ExitCode != 0 {
		p.add("available", CheckBlock, "do-release-upgrade is not installed (package ubuntu-release-upgrader-core).")
		return
	}
	body, err := s.fetch(ctx, s.metaReleaseURL)
	if err != nil {
		p.add("available", CheckBlock, "Could not read Ubuntu's release list: "+err.Error())
		return
	}
	version, supported := parseMetaRelease(string(body), ToCodename)
	if version == "" || !supported {
		p.add("available", CheckBlock, "Ubuntu "+ToVersion+" is not offered as an LTS upgrade yet.")
		return
	}
	p.add("available", CheckOK, "Ubuntu "+version+" is available.")
}

// parseMetaRelease finds the stanza for dist and returns its version and
// whether it is marked supported.
func parseMetaRelease(content, dist string) (string, bool) {
	for _, stanza := range strings.Split(content, "\n\n") {
		fields := map[string]string{}
		for _, line := range strings.Split(stanza, "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok {
				fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		if fields["Dist"] == dist {
			return fields["Version"], fields["Supported"] == "1"
		}
	}
	return "", false
}

func (s *Service) checkPendingUpdates(ctx context.Context, p *Preflight) {
	res, err := s.exec.RunSudo(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "dist-upgrade")
	if err != nil || res == nil || res.ExitCode != 0 {
		detail := ""
		if res != nil {
			detail = strings.TrimSpace(res.Stderr)
		}
		p.add("pending_updates", CheckBlock, "Could not check pending updates. "+detail)
		return
	}
	pending := 0
	for _, line := range strings.Split(res.Stdout, "\n") {
		if strings.HasPrefix(line, "Inst ") {
			pending++
		}
	}
	if pending > 0 {
		p.add("pending_updates", CheckBlock, fmt.Sprintf("%d package update(s) are pending. Run apt upgrade first (the upgrader refuses otherwise).", pending))
		return
	}
	p.add("pending_updates", CheckOK, "All packages are up to date.")
}

func (s *Service) checkRebootRequired(ctx context.Context, p *Preflight) {
	if res, err := s.exec.Run(ctx, "/usr/bin/test", "-e", "/var/run/reboot-required"); err == nil && res != nil && res.ExitCode == 0 {
		p.add("reboot_required", CheckBlock, "A reboot from earlier updates is pending. Reboot first.")
		return
	}
	p.add("reboot_required", CheckOK, "No reboot pending.")
}

func (s *Service) checkDpkg(ctx context.Context, p *Preflight) {
	if res, err := s.exec.RunSudo(ctx, "dpkg", "--audit"); err == nil && res != nil && strings.TrimSpace(res.Stdout) != "" {
		p.add("dpkg", CheckBlock, "dpkg reports broken or half-configured packages. Fix them first (dpkg --configure -a).")
		return
	}
	if res, err := s.exec.RunSudo(ctx, "fuser", "/var/lib/dpkg/lock-frontend"); err == nil && res != nil && res.ExitCode == 0 {
		p.add("dpkg", CheckBlock, "Another package operation is running.")
		return
	}
	p.add("dpkg", CheckOK, "The package system is consistent and idle.")
}

func (s *Service) checkDisk(ctx context.Context, p *Preflight) {
	res, err := s.exec.Run(ctx, "df", "-B1", "--output=avail", "/", "/boot")
	if err != nil || res == nil || res.ExitCode != 0 {
		p.add("disk", CheckBlock, "Could not read free disk space.")
		return
	}
	var avail []int64
	for _, line := range strings.Split(res.Stdout, "\n") {
		if n, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil {
			avail = append(avail, n)
		}
	}
	if len(avail) < 2 {
		p.add("disk", CheckBlock, "Could not read free disk space.")
		return
	}
	switch {
	case avail[0] < minRootFree:
		p.add("disk", CheckBlock, fmt.Sprintf("/ has %s free; at least 5 GB is needed.", humanBytes(avail[0])))
	case avail[1] < minBootFree:
		p.add("disk", CheckBlock, fmt.Sprintf("/boot has %s free; at least 300 MB is needed.", humanBytes(avail[1])))
	default:
		p.add("disk", CheckOK, fmt.Sprintf("%s free on /.", humanBytes(avail[0])))
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// sourcesDumpScript prints every apt source file with a header line. It is
// fixed text with no user input.
const sourcesDumpScript = `for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
    [ -f "$f" ] || continue
    printf '==> %s\n' "$f"
    cat "$f"
    printf '\n'
done`

func (s *Service) readSources(ctx context.Context) ([]AptSource, error) {
	res, err := s.exec.RunSudo(ctx, "/bin/bash", "-c", sourcesDumpScript)
	if err != nil {
		return nil, err
	}
	if res == nil || res.ExitCode != 0 {
		return nil, fmt.Errorf("read apt sources failed")
	}
	var sources []AptSource
	var file string
	var body strings.Builder
	flush := func() {
		if file == "" {
			return
		}
		if strings.HasSuffix(file, ".sources") {
			sources = append(sources, ParseDeb822File(file, body.String())...)
		} else {
			sources = append(sources, ParseListFile(file, body.String())...)
		}
		body.Reset()
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if name, ok := strings.CutPrefix(line, "==> "); ok {
			flush()
			file = name
			continue
		}
		body.WriteString(line + "\n")
	}
	flush()
	return sources, nil
}

// checkSources verifies that every enabled source has a resolute
// equivalent. The panel's own repositories (sury, pgdg) must have one;
// for anything else a missing release is a warning, because the upgrader
// then disables that entry and (with RemoveObsoletes=no) keeps its packages.
func (s *Service) checkSources(ctx context.Context, p *Preflight) {
	sources, err := s.readSources(ctx)
	if err != nil {
		p.add("sources", CheckBlock, "Could not read the apt sources: "+err.Error())
		return
	}
	var blocked, disabled, foreign []string
	seen := map[string]bool{}
	for _, src := range sources {
		if !src.HasBinary() {
			continue
		}
		for _, suite := range src.Suites {
			key := src.URI + " " + suite
			if seen[key] {
				continue
			}
			seen[key] = true
			target, ok := MapSuite(suite)
			if !ok {
				disabled = append(disabled, key)
				continue
			}
			if !src.UbuntuArchive() && !src.PanelManaged() && !contains(foreign, src.URI) {
				foreign = append(foreign, src.URI)
			}
			if s.releaseExists(ctx, src.URI, target) {
				continue
			}
			if src.PanelManaged() {
				blocked = append(blocked, src.URI+" "+target)
			} else {
				disabled = append(disabled, src.URI+" "+target)
			}
		}
	}
	if len(blocked) > 0 {
		p.add("sources", CheckBlock, "These panel repositories do not offer "+ToCodename+" yet: "+strings.Join(blocked, ", "))
	} else if len(disabled) > 0 {
		p.add("sources", CheckWarn, "These sources have no "+ToCodename+" release and will be disabled by the upgrader (their packages stay installed): "+strings.Join(disabled, ", "))
	} else {
		p.add("sources", CheckOK, "Every apt source offers "+ToCodename+".")
	}
	if len(foreign) > 0 {
		p.add("foreign_sources", CheckWarn, "Third-party sources not managed by the panel will be moved to "+ToCodename+": "+strings.Join(foreign, ", "))
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// releaseExists asks the repository for the target suite's InRelease (or
// Release) file.
func (s *Service) releaseExists(ctx context.Context, uri, suite string) bool {
	base := strings.TrimRight(uri, "/") + "/dists/" + suite + "/"
	for _, name := range []string{"InRelease", "Release"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, base+name, nil)
		if err != nil {
			return false
		}
		resp, err := s.http.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return true
		}
	}
	return false
}

// sitePHPVersions lists the PHP versions used by sites.
func (s *Service) sitePHPVersions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT php_version FROM websites WHERE php_version IS NOT NULL AND php_version != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		if phpVersionPattern(v) {
			versions = append(versions, v)
		}
	}
	sort.Strings(versions)
	return versions, rows.Err()
}

func phpVersionPattern(v string) bool {
	major, minor, ok := strings.Cut(v, ".")
	if !ok || major == "" || minor == "" || len(major) > 1 || len(minor) > 2 {
		return false
	}
	for _, r := range major + minor {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// checkPHP makes sure packages.sury.org publishes every PHP version a site
// uses for resolute, so no site loses its PHP-FPM.
func (s *Service) checkPHP(ctx context.Context, p *Preflight) {
	versions, err := s.sitePHPVersions(ctx)
	if err != nil {
		p.add("php", CheckBlock, "Could not read the sites' PHP versions: "+err.Error())
		return
	}
	p.phpVersions = versions
	if len(versions) == 0 {
		p.add("php", CheckOK, "No site uses PHP.")
		return
	}
	body, err := s.fetch(ctx, s.suryPackagesURL)
	if err != nil {
		p.add("php", CheckBlock, "Could not read packages.sury.org for "+ToCodename+": "+err.Error())
		return
	}
	published, err := fpmPackages(body)
	if err != nil {
		p.add("php", CheckBlock, "Could not read packages.sury.org for "+ToCodename+": "+err.Error())
		return
	}
	var missing []string
	for _, v := range versions {
		if !published["php"+v+"-fpm"] {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		p.add("php", CheckBlock, "PHP "+strings.Join(missing, ", ")+" is used by sites but not published for "+ToCodename+".")
		return
	}
	p.add("php", CheckOK, "PHP "+strings.Join(versions, ", ")+" will stay available.")
}

// fpmPackages reads a gzip-compressed Packages index and returns the
// php*-fpm package names it lists.
func fpmPackages(gz []byte) (map[string]bool, error) {
	zr, err := gzip.NewReader(strings.NewReader(string(gz)))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	found := map[string]bool{}
	scanner := bufio.NewScanner(zr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if name, ok := strings.CutPrefix(scanner.Text(), "Package: "); ok && strings.HasPrefix(name, "php") && strings.HasSuffix(name, "-fpm") {
			found[name] = true
		}
	}
	return found, scanner.Err()
}

// nativePasswordQuery counts accounts using mysql_native_password, which
// MySQL 8.4 (resolute) disables by default. Built-in accounts are excluded.
const nativePasswordQuery = `SELECT COUNT(*) FROM mysql.user WHERE plugin = 'mysql_native_password' AND user NOT IN ('mysql.sys', 'mysql.session', 'mysql.infoschema')`

func (s *Service) checkMySQL(ctx context.Context, p *Preflight) {
	if res, err := s.exec.Run(ctx, "/usr/bin/test", "-x", "/usr/sbin/mysqld"); err != nil || res == nil || res.ExitCode != 0 {
		return // MySQL is not installed
	}
	res, err := s.exec.RunSudo(ctx, "mysql", "--batch", "--skip-column-names", "-e", nativePasswordQuery)
	if err != nil || res == nil || res.ExitCode != 0 {
		p.add("mysql_native_password", CheckWarn, "Could not check MySQL users for mysql_native_password; it will be re-enabled after the upgrade to be safe.")
		p.mysqlNative = true
		return
	}
	n, _ := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if n > 0 {
		p.mysqlNative = true
		p.add("mysql_native_password", CheckWarn, fmt.Sprintf("%d MySQL user(s) use mysql_native_password, which MySQL 8.4 disables by default. The panel re-enables it after the upgrade.", n))
		return
	}
	p.add("mysql_native_password", CheckOK, "MySQL 8.0 will be upgraded to 8.4; no user depends on mysql_native_password.")
}

// fetch GETs a URL with the service's client, capped at 64 MiB.
func (s *Service) fetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}
