// Package depaudit checks the Composer and npm dependencies of the panel's
// websites against OSV.dev. Lockfiles are parsed here; no package manager
// or project script ever runs in a site directory.
package depaudit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// OSV ecosystem names.
const (
	EcosystemPackagist = "Packagist"
	EcosystemNPM       = "npm"
)

// Dependency is one resolved package from a lockfile.
type Dependency struct {
	Ecosystem string
	Name      string
	Version   string
	Dev       bool
	Lockfile  string // base name, e.g. "composer.lock"
}

// Lockfiles are the lockfile names looked for in a project directory.
var Lockfiles = []string{"composer.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml"}

// ParseLockfile parses a lockfile by its base name.
func ParseLockfile(name string, data []byte) ([]Dependency, error) {
	var deps []Dependency
	var err error
	switch name {
	case "composer.lock":
		deps, err = parseComposerLock(data)
	case "package-lock.json":
		deps, err = parsePackageLock(data)
	case "yarn.lock":
		deps, err = parseYarnLock(data)
	case "pnpm-lock.yaml":
		deps, err = parsePnpmLock(data)
	default:
		return nil, fmt.Errorf("unsupported lockfile %q", name)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	for i := range deps {
		deps[i].Lockfile = name
	}
	return dedupe(deps), nil
}

var versionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*([-+][0-9A-Za-z.+-]+)?$`)

// registryVersion normalizes a version published to a registry ("v1.2.0"
// -> "1.2.0") and rejects branches, paths, URLs and workspace links.
func registryVersion(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if len(v) > 1 && (v[0] == 'v' || v[0] == 'V') && v[1] >= '0' && v[1] <= '9' {
		v = v[1:]
	}
	if !versionPattern.MatchString(v) || strings.HasSuffix(v, "-dev") {
		return "", false
	}
	return v, true
}

func add(deps []Dependency, eco, name, version string, dev bool) []Dependency {
	name = strings.TrimSpace(name)
	if name == "" {
		return deps
	}
	v, ok := registryVersion(version)
	if !ok {
		return deps
	}
	return append(deps, Dependency{Ecosystem: eco, Name: name, Version: v, Dev: dev})
}

// dedupe collapses identical packages; one is dev only when every
// occurrence is.
func dedupe(deps []Dependency) []Dependency {
	index := map[string]int{}
	var out []Dependency
	for _, d := range deps {
		key := d.Ecosystem + "\x00" + d.Name + "\x00" + d.Version
		if i, ok := index[key]; ok {
			out[i].Dev = out[i].Dev && d.Dev
			continue
		}
		index[key] = len(out)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func parseComposerLock(data []byte) ([]Dependency, error) {
	var lock struct {
		Packages    []struct{ Name, Version string } `json:"packages"`
		PackagesDev []struct{ Name, Version string } `json:"packages-dev"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	var deps []Dependency
	for _, p := range lock.Packages {
		deps = add(deps, EcosystemPackagist, p.Name, p.Version, false)
	}
	for _, p := range lock.PackagesDev {
		deps = add(deps, EcosystemPackagist, p.Name, p.Version, true)
	}
	return deps, nil
}

type npmV1Dep struct {
	Version      string              `json:"version"`
	Dev          bool                `json:"dev"`
	Dependencies map[string]npmV1Dep `json:"dependencies"`
}

func parsePackageLock(data []byte) ([]Dependency, error) {
	var lock struct {
		Packages map[string]struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Dev     bool   `json:"dev"`
			Link    bool   `json:"link"`
		} `json:"packages"`
		Dependencies map[string]npmV1Dep `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	var deps []Dependency
	if len(lock.Packages) > 0 {
		for key, p := range lock.Packages {
			i := strings.LastIndex(key, "node_modules/")
			if key == "" || p.Link || i < 0 {
				continue // the root project, workspace packages and links
			}
			deps = add(deps, EcosystemNPM, key[i+len("node_modules/"):], p.Version, p.Dev)
		}
		return deps, nil
	}
	var walk func(map[string]npmV1Dep)
	walk = func(m map[string]npmV1Dep) {
		for name, d := range m {
			deps = add(deps, EcosystemNPM, name, d.Version, d.Dev)
			walk(d.Dependencies)
		}
	}
	walk(lock.Dependencies)
	return deps, nil
}

func parseYarnLock(data []byte) ([]Dependency, error) {
	if bytes.Contains(data, []byte("__metadata:")) {
		return parseYarnBerry(data)
	}
	var deps []Dependency
	var name string
	skip := true
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line[0] != ' ' {
			// Block header: `"@scope/a@^1", "@scope/a@^1.2":`
			first := strings.TrimSpace(strings.Split(strings.TrimSuffix(line, ":"), ",")[0])
			first = strings.Trim(first, `"`)
			at := strings.LastIndex(first, "@")
			if at <= 0 {
				name, skip = "", true
				continue
			}
			name = first[:at]
			rng := first[at+1:]
			skip = strings.Contains(rng, ":") || strings.Contains(rng, "/")
			continue
		}
		if skip || name == "" {
			continue
		}
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "version "); ok {
			deps = add(deps, EcosystemNPM, name, strings.Trim(v, `"`), false)
			name = ""
		}
	}
	return deps, scanner.Err()
}

func parseYarnBerry(data []byte) ([]Dependency, error) {
	var lock map[string]struct {
		Version    string `yaml:"version"`
		Resolution string `yaml:"resolution"`
	}
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	var deps []Dependency
	for key, entry := range lock {
		if key == "__metadata" {
			continue
		}
		name, _, ok := strings.Cut(entry.Resolution, "@npm:")
		if !ok {
			continue // workspace:, patch:, git and file resolutions
		}
		deps = add(deps, EcosystemNPM, name, entry.Version, false)
	}
	return deps, nil
}

func parsePnpmLock(data []byte) ([]Dependency, error) {
	var lock struct {
		Packages map[string]struct {
			Dev     *bool  `yaml:"dev"`
			Version string `yaml:"version"`
		} `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	var deps []Dependency
	for key, p := range lock.Packages {
		name, version := pnpmKey(key)
		if p.Version != "" && version == "" {
			version = p.Version
		}
		dev := p.Dev != nil && *p.Dev
		deps = add(deps, EcosystemNPM, name, version, dev)
	}
	return deps, nil
}

// pnpmKey splits "/vite@5.2.0(@types/node@20.11.0)" (v6),
// "vite@5.2.0" (v9) or "/vite/5.2.0" (v5) into name and version.
func pnpmKey(key string) (string, string) {
	key = strings.TrimPrefix(key, "/")
	if i := strings.Index(key, "("); i >= 0 {
		key = key[:i]
	}
	if at := strings.LastIndex(key, "@"); at > 0 {
		return key[:at], key[at+1:]
	}
	// v5: name/version, the name possibly scoped (@scope/name/version).
	if i := strings.LastIndex(key, "/"); i > 0 {
		version := key[i+1:]
		if j := strings.Index(version, "_"); j >= 0 {
			version = version[:j]
		}
		return key[:i], version
	}
	return "", ""
}
