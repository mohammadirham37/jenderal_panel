// Package releaseupgrade upgrades a panel server from Ubuntu 24.04 (noble)
// to 26.04 (resolute) with Canonical's do-release-upgrade, guarded by
// preflight checks and followed by a repair of what the panel manages.
// See docs/superpowers/specs/2026-11-22-ubuntu-release-upgrade-design.md.
package releaseupgrade

import (
	"strings"
)

const (
	// FromCodename and ToCodename are the only upgrade path offered.
	FromCodename = "noble"
	ToCodename   = "resolute"
	FromVersion  = "24.04"
	ToVersion    = "26.04"
)

// upgradePockets are the suite suffixes the upgrader moves to the new
// release. "pgdg" is not one of Ubuntu's pockets; the panel adds it to the
// upgrader's Pockets setting so noble-pgdg maps to resolute-pgdg.
var upgradePockets = []string{"security", "updates", "proposed", "backports", "pgdg"}

// AptSource is one enabled apt source entry.
type AptSource struct {
	File   string   `json:"file"`
	URI    string   `json:"uri"`
	Suites []string `json:"suites"`
	Types  []string `json:"types"`
}

// MapSuite returns the suite an entry uses after the upgrade, mirroring the
// upgrader's mapping: noble and noble-<pocket> move to resolute; suites that
// already name resolute stay. ok is false for any other suite (the upgrader
// disables such entries).
func MapSuite(suite string) (string, bool) {
	if suite == FromCodename || suite == ToCodename {
		return ToCodename, true
	}
	for _, pocket := range upgradePockets {
		if suite == FromCodename+"-"+pocket || suite == ToCodename+"-"+pocket {
			return ToCodename + "-" + pocket, true
		}
	}
	return "", false
}

// ParseListFile parses the one-line format (sources.list, *.list). Comments,
// commented-out entries and malformed lines are skipped.
func ParseListFile(file, content string) []AptSource {
	var sources []AptSource
	for _, line := range strings.Split(content, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || (fields[0] != "deb" && fields[0] != "deb-src") {
			continue
		}
		kind := fields[0]
		rest := fields[1:]
		if strings.HasPrefix(rest[0], "[") {
			// Options run until the field that closes the bracket.
			for len(rest) > 0 {
				closed := strings.HasSuffix(rest[0], "]")
				rest = rest[1:]
				if closed {
					break
				}
			}
		}
		if len(rest) < 2 {
			continue
		}
		sources = append(sources, AptSource{File: file, URI: rest[0], Suites: []string{rest[1]}, Types: []string{kind}})
	}
	return sources
}

// ParseDeb822File parses the deb822 format (*.sources). Stanzas with
// "Enabled: no" are skipped.
func ParseDeb822File(file, content string) []AptSource {
	var sources []AptSource
	for _, stanza := range splitStanzas(content) {
		fields := map[string]string{}
		var last string
		for _, line := range stanza {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				if last != "" {
					fields[last] += " " + strings.TrimSpace(line)
				}
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			last = strings.ToLower(strings.TrimSpace(key))
			fields[last] = strings.TrimSpace(value)
		}
		if strings.EqualFold(fields["enabled"], "no") {
			continue
		}
		uris := strings.Fields(fields["uris"])
		suites := strings.Fields(fields["suites"])
		types := strings.Fields(fields["types"])
		if len(uris) == 0 || len(suites) == 0 || len(types) == 0 {
			continue
		}
		for _, uri := range uris {
			sources = append(sources, AptSource{File: file, URI: uri, Suites: suites, Types: types})
		}
	}
	return sources
}

// splitStanzas splits deb822 content into stanzas of non-comment lines.
func splitStanzas(content string) [][]string {
	var stanzas [][]string
	var current []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				stanzas = append(stanzas, current)
				current = nil
			}
			continue
		}
		current = append(current, strings.TrimRight(line, "\r"))
	}
	if len(current) > 0 {
		stanzas = append(stanzas, current)
	}
	return stanzas
}

// HasBinary reports whether the entry provides binary packages; deb-src
// entries do not matter for the upgrade.
func (s AptSource) HasBinary() bool {
	for _, t := range s.Types {
		if t == "deb" {
			return true
		}
	}
	return false
}

// panelManagedHosts are repositories the panel adds itself and re-points at
// the new release after the upgrade.
var panelManagedHosts = []string{"packages.sury.org", "apt.postgresql.org"}

// ubuntuHosts are Canonical's own archives; any other host is a mirror or a
// third-party repository.
var ubuntuHosts = []string{"archive.ubuntu.com", "security.ubuntu.com", "ports.ubuntu.com"}

func uriHost(uri string) string {
	rest := uri
	if _, after, ok := strings.Cut(uri, "://"); ok {
		rest = after
	}
	host, _, _ := strings.Cut(rest, "/")
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	return strings.ToLower(host)
}

func hostMatches(host string, list []string) bool {
	for _, h := range list {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// PanelManaged reports whether the entry is a repository the panel manages.
func (s AptSource) PanelManaged() bool { return hostMatches(uriHost(s.URI), panelManagedHosts) }

// UbuntuArchive reports whether the entry points at Canonical's archive.
func (s AptSource) UbuntuArchive() bool { return hostMatches(uriHost(s.URI), ubuntuHosts) }
