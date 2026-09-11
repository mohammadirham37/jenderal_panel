package website

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// phpSettingsAllowlist defines the per-site PHP settings the panel manages
// (written to the site's .user.ini) and the value shapes each accepts.
var phpSettingsAllowlist = map[string]*regexp.Regexp{
	"memory_limit":        regexp.MustCompile(`^-?\d+[KMG]$`),
	"upload_max_filesize": regexp.MustCompile(`^\d+[KMG]$`),
	"post_max_size":       regexp.MustCompile(`^\d+[KMG]$`),
	"max_execution_time":  regexp.MustCompile(`^\d+$`),
	"max_input_time":      regexp.MustCompile(`^\d+$`),
	"max_input_vars":      regexp.MustCompile(`^\d+$`),
}

const userIniFilename = ".user.ini"

// GetPhpSettings reads the site's .user.ini and returns the managed settings
// (empty string when not set).
func (s *Service) GetPhpSettings(ctx context.Context, websiteID string) (map[string]string, error) {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return nil, err
	}
	if w.AppType == "static" {
		return nil, fmt.Errorf("static sites do not use PHP settings")
	}

	iniPath := filepath.Join(w.DocumentRoot, userIniFilename)
	result, err := s.exec.RunSudo(ctx, "cat", iniPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", userIniFilename, err)
	}

	settings := make(map[string]string, len(phpSettingsAllowlist))
	for key := range phpSettingsAllowlist {
		settings[key] = ""
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if _, managed := phpSettingsAllowlist[key]; managed {
			settings[key] = strings.TrimSpace(parts[1])
		}
	}
	return settings, nil
}

// SavePhpSettings validates and writes the managed settings to the site's
// .user.ini, then reloads the site's PHP-FPM pool so the values apply
// immediately (user_ini.cache_ttl would otherwise delay them by minutes).
func (s *Service) SavePhpSettings(ctx context.Context, websiteID string, settings map[string]string) error {
	w, err := s.Get(ctx, websiteID)
	if err != nil {
		return err
	}
	if w.AppType == "static" {
		return fmt.Errorf("static sites do not use PHP settings")
	}

	// Only managed keys with allow-listed value shapes are accepted.
	var unknown []string
	for key, value := range settings {
		pattern, managed := phpSettingsAllowlist[key]
		if !managed {
			unknown = append(unknown, key)
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" && !pattern.MatchString(value) {
			return fmt.Errorf("invalid value for %s: %q", key, value)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unsupported PHP settings: %s", strings.Join(unknown, ", "))
	}

	iniPath := filepath.Join(w.DocumentRoot, userIniFilename)

	// Write via stdin (root) and hand the file back to the web user.
	var b strings.Builder
	b.WriteString("; Managed by Jenderal Panel — edited values only\n")
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(settings[key]); value != "" {
			fmt.Fprintf(&b, "%s = %s\n", key, value)
		}
	}
	if _, err := s.exec.RunSudoWithInput(ctx, b.String(), "tee", iniPath); err != nil {
		return fmt.Errorf("write %s: %w", userIniFilename, err)
	}
	if _, err := s.exec.RunSudo(ctx, "chown", w.WebUser+":"+w.WebUser, iniPath); err != nil {
		return fmt.Errorf("set %s ownership: %w", userIniFilename, err)
	}

	if _, err := s.exec.RunSudo(ctx, "systemctl", "reload",
		"php"+w.PHPVersion+"-fpm-"+w.Domain); err != nil {
		return fmt.Errorf("reload php-fpm: %w", err)
	}
	return nil
}
