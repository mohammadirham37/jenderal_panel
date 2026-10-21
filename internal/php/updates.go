package php

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// securityBaselines is the lowest upstream patch release per PHP branch that
// carries the latest known security fixes. Ondrej PPA packages track upstream
// version numbers, so on jammy/noble an installed version below the baseline
// is definitely vulnerable. The Ubuntu archive (resolute) backports fixes
// without bumping the upstream number, so baselines are not applied there.
//
// 2026-10-21: PHP 8.5.12 / 8.4.27 / 8.3.36 / 8.2.35 fix CVE-2026-109618 and
// CVE-2026-109619 (unauthenticated RCE, CVSS 9.2).
// https://www.php.net/archive/2026.php#2026-10-21-5
var securityBaselines = map[string]string{
	"8.2": "8.2.35",
	"8.3": "8.3.36",
	"8.4": "8.4.27",
	"8.5": "8.5.12",
}

// securityAdvisoryURL points at the advisory behind securityBaselines.
const securityAdvisoryURL = "https://www.php.net/archive/2026.php#2026-10-21-5"

// endOfLifeBranches no longer receive upstream security fixes at all.
var endOfLifeBranches = map[string]bool{"8.1": true}

// errNothingToUpgrade is returned when none of the requested versions has
// installed packages.
var errNothingToUpgrade = model.NewValidationError("no installed PHP packages to upgrade")

// upstreamVersionRegex extracts "8.3.12" from a Debian version such as
// "1:8.3.12-1+ubuntu24.04.1+deb.sury.org+1" or "8.5.4-1ubuntu1".
var upstreamVersionRegex = regexp.MustCompile(`^(?:\d+:)?(\d+\.\d+\.\d+)`)

// PackageUpdate is one installed php<ver>-* package with a newer candidate.
type PackageUpdate struct {
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Candidate string `json:"candidate"`
}

// PatchStatus describes the patch level of one installed PHP version.
type PatchStatus struct {
	Version         string          `json:"version"`
	Current         string          `json:"current"`
	Latest          string          `json:"latest"`
	UpdateAvailable bool            `json:"update_available"`
	Packages        []PackageUpdate `json:"packages"`
	// SecurityBaseline is the minimum safe upstream release ("" when not
	// tracked for this branch or OS); Vulnerable is set when Current is lower.
	SecurityBaseline string `json:"security_baseline"`
	Vulnerable       bool   `json:"vulnerable"`
	EndOfLife        bool   `json:"end_of_life"`
	AdvisoryURL      string `json:"advisory_url,omitempty"`
}

// PatchOverview is the response for GET /php/updates.
type PatchOverview struct {
	Versions []PatchStatus `json:"versions"`
	// ListsUpdatedAt is when apt last refreshed its package lists; candidates
	// are only as fresh as this timestamp.
	ListsUpdatedAt *time.Time `json:"lists_updated_at"`
}

// upstreamVersion returns the x.y.z part of a Debian package version.
func upstreamVersion(debVersion string) string {
	if m := upstreamVersionRegex.FindStringSubmatch(strings.TrimSpace(debVersion)); m != nil {
		return m[1]
	}
	return ""
}

// compareUpstream compares dotted numeric versions; -1, 0, or 1.
func compareUpstream(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// installedPackages lists fully installed ("ii") php<ver>-* packages with
// their Debian versions.
func (s *Service) installedPackages(ctx context.Context, version string) map[string]string {
	pkgs := map[string]string{}
	res, err := s.exec.Run(ctx, "/usr/bin/dpkg-query", "-W",
		"-f=${binary:Package}\t${Version}\t${db:Status-Abbrev}\n", "php"+version+"-*")
	if err != nil || res == nil {
		return pkgs
	}
	// dpkg-query exits 1 when some glob matches are not installed; the
	// listed rows are still valid.
	for _, line := range strings.Split(res.Stdout, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || !strings.HasPrefix(fields[2], "ii") || fields[1] == "" {
			continue
		}
		name, _, _ := strings.Cut(fields[0], ":") // drop a ":amd64" qualifier
		pkgs[name] = fields[1]
	}
	return pkgs
}

// parsePolicy reads `apt-cache policy` output into package → candidate.
func parsePolicy(output string) map[string]string {
	candidates := map[string]string{}
	current := ""
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":") {
			current = strings.TrimSuffix(line, ":")
			continue
		}
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "Candidate:"); ok && current != "" {
			value = strings.TrimSpace(value)
			if value != "(none)" {
				candidates[current] = value
			}
		}
	}
	return candidates
}

// patchStatus builds the status of one installed version from dpkg and the
// cached apt lists. It never touches the network.
func (s *Service) patchStatus(ctx context.Context, version string, applyBaseline bool) PatchStatus {
	status := PatchStatus{Version: version, Packages: []PackageUpdate{}, EndOfLife: endOfLifeBranches[version]}
	installed := s.installedPackages(ctx, version)
	if len(installed) == 0 {
		return status
	}

	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	sort.Strings(names)

	candidates := map[string]string{}
	args := append([]string{"LC_ALL=C", "/usr/bin/apt-cache", "policy"}, names...)
	if res, err := s.exec.Run(ctx, "/usr/bin/env", args...); err == nil && res != nil && res.ExitCode == 0 {
		candidates = parsePolicy(res.Stdout)
	}

	// The common package carries the engine version every SAPI shares.
	reference := "php" + version + "-common"
	if _, ok := installed[reference]; !ok {
		reference = names[0]
	}
	status.Current = upstreamVersion(installed[reference])
	status.Latest = status.Current
	if c := upstreamVersion(candidates[reference]); c != "" && compareUpstream(c, status.Latest) > 0 {
		status.Latest = c
	}

	for _, name := range names {
		candidate := candidates[name]
		if candidate == "" || candidate == installed[name] {
			continue
		}
		status.Packages = append(status.Packages, PackageUpdate{Name: name, Installed: installed[name], Candidate: candidate})
	}
	status.UpdateAvailable = len(status.Packages) > 0

	if baseline, ok := securityBaselines[version]; ok && applyBaseline && status.Current != "" {
		status.SecurityBaseline = baseline
		status.AdvisoryURL = securityAdvisoryURL
		status.Vulnerable = compareUpstream(status.Current, baseline) < 0
	}
	return status
}

// PatchOverview reports the patch level of every installed PHP version.
func (s *Service) PatchOverview(ctx context.Context) (PatchOverview, error) {
	overview := PatchOverview{Versions: []PatchStatus{}}
	codename := Codename(ctx, s.exec)
	applyBaseline := codename != "resolute"
	for _, version := range VersionsForCodename(codename) {
		if !s.IsInstalled(ctx, version) {
			continue
		}
		overview.Versions = append(overview.Versions, s.patchStatus(ctx, version, applyBaseline))
	}
	if res, err := s.exec.Run(ctx, "/usr/bin/stat", "-c", "%Y", "/var/cache/apt/pkgcache.bin"); err == nil && res != nil && res.ExitCode == 0 {
		if secs, err := strconv.ParseInt(strings.TrimSpace(res.Stdout), 10, 64); err == nil && secs > 0 {
			t := time.Unix(secs, 0).UTC()
			overview.ListsUpdatedAt = &t
		}
	}
	return overview, nil
}

// refreshListsCommands refreshes apt package lists so candidates are current.
// The repository setup script re-asserts the ondrej source first, so a
// server whose PPA entry went missing still sees upstream patch releases.
func (s *Service) refreshListsCommands() [][]string {
	updateArgs := append([]string{"update", "-qq"}, aptTimeoutArgs()...)
	return [][]string{
		{"bash", "-c", phpRepositorySetupScript},
		append([]string{"apt-get"}, updateArgs...),
	}
}

// upgradeCommands upgrades only the already-installed php<ver>-* packages of
// each version (never installs new ones), keeps locally edited php.ini and
// pool files, then restarts each running FPM service so workers load the
// patched engine.
func (s *Service) upgradeCommands(ctx context.Context, versions []string) ([][]string, error) {
	upgradeArgs := append([]string{
		"install", "--only-upgrade", "-y",
		"-o", "DPkg::Lock::Timeout=120",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
	}, aptTimeoutArgs()...)

	var packages, restart []string
	for _, version := range versions {
		installed := s.installedPackages(ctx, version)
		if len(installed) == 0 {
			continue
		}
		names := make([]string, 0, len(installed))
		for name := range installed {
			names = append(names, name)
		}
		sort.Strings(names)
		packages = append(packages, names...)
		if _, ok := installed["php"+version+"-fpm"]; ok {
			restart = append(restart, "php"+version+"-fpm")
		}
	}
	if len(packages) == 0 {
		return nil, errNothingToUpgrade
	}

	commands := s.refreshListsCommands()
	commands = append(commands, append(append([]string{"apt-get"}, upgradeArgs...), packages...))
	for _, unit := range restart {
		// try-restart only touches services that are currently running.
		commands = append(commands, []string{"systemctl", "try-restart", unit})
	}
	return commands, nil
}
