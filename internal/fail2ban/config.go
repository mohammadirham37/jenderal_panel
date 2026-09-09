package fail2ban

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const managedConfigPath = "/etc/fail2ban/jail.d/jenderal-panel.local"

var knownJails = map[string]string{
	"sshd":            "sshd",
	"nginx-http-auth": "nginx-http-auth",
	"nginx-limit-req": "nginx-limit-req",
	"nginx-botsearch": "nginx-botsearch",
	"nginx-badbots":   "nginx-badbots",
}

func ParseSettings(content string) (Settings, error) {
	settings := SafeSettings()
	settings.SSHDEnabled = false
	settings.EnabledJails = nil
	settings.IgnoreIPs = nil
	section := ""
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.SplitN(rawLine, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if section == "DEFAULT" {
			switch key {
			case "ignoreip":
				settings.IgnoreIPs = strings.Fields(value)
			case "maxretry":
				settings.MaxRetry, _ = strconv.Atoi(value)
			case "findtime":
				settings.FindTimeSeconds, _ = strconv.Atoi(value)
			case "bantime":
				settings.BanTimeSeconds, _ = strconv.Atoi(value)
			}
			continue
		}
		if key == "enabled" && strings.EqualFold(value, "true") {
			if section == "sshd" {
				settings.SSHDEnabled = true
			} else if _, allowed := knownJails[section]; allowed {
				settings.EnabledJails = append(settings.EnabledJails, section)
			}
		}
	}
	if _, _, err := validateSettings(settings); err != nil {
		return Settings{}, fmt.Errorf("parse managed fail2ban settings: %w", err)
	}
	return settings, nil
}

func SafeSettings() Settings {
	return Settings{
		SSHDEnabled: true, MaxRetry: 5, FindTimeSeconds: 600, BanTimeSeconds: 900,
		IgnoreIPs: []string{"127.0.0.1/8", "::1/128"},
	}
}

func Render(settings Settings) (string, error) {
	ignoreIPs, jails, err := validateSettings(settings)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	output.WriteString("# Managed by Jenderal Panel. Edit through the panel.\n")
	output.WriteString("[DEFAULT]\n")
	fmt.Fprintf(&output, "ignoreip = %s\n", strings.Join(ignoreIPs, " "))
	fmt.Fprintf(&output, "maxretry = %d\n", settings.MaxRetry)
	fmt.Fprintf(&output, "findtime = %d\n", settings.FindTimeSeconds)
	fmt.Fprintf(&output, "bantime = %d\n", settings.BanTimeSeconds)
	for _, jail := range jails {
		fmt.Fprintf(&output, "\n[%s]\nenabled = true\n", jail)
	}
	return output.String(), nil
}

func validateSettings(settings Settings) ([]string, []string, error) {
	if settings.MaxRetry < 1 || settings.MaxRetry > 20 {
		return nil, nil, model.NewValidationError("max retry must be between 1 and 20")
	}
	if settings.FindTimeSeconds < 60 || settings.FindTimeSeconds > 86400 {
		return nil, nil, model.NewValidationError("find time must be between 60 and 86400 seconds")
	}
	if settings.BanTimeSeconds < 60 || settings.BanTimeSeconds > 604800 {
		return nil, nil, model.NewValidationError("ban time must be temporary and between 60 and 604800 seconds")
	}

	ignoreIPs := make([]string, 0, len(settings.IgnoreIPs)+2)
	seenNetworks := make(map[string]struct{})
	addNetwork := func(value string) error {
		value = strings.TrimSpace(value)
		if value == "" {
			return model.NewValidationError("ignore IP entries must not be empty")
		}
		canonical := ""
		if prefix, err := netip.ParsePrefix(value); err == nil {
			canonical = prefix.String()
		} else if address, err := netip.ParseAddr(value); err == nil {
			canonical = address.String()
		} else {
			return model.NewValidationError("invalid ignore IP or CIDR: " + value)
		}
		if _, exists := seenNetworks[canonical]; !exists {
			seenNetworks[canonical] = struct{}{}
			ignoreIPs = append(ignoreIPs, canonical)
		}
		return nil
	}
	for _, value := range settings.IgnoreIPs {
		if err := addNetwork(value); err != nil {
			return nil, nil, err
		}
	}
	for _, loopback := range []string{"127.0.0.1/8", "::1/128"} {
		if err := addNetwork(loopback); err != nil {
			return nil, nil, err
		}
	}

	jailSet := make(map[string]struct{})
	if settings.SSHDEnabled {
		jailSet["sshd"] = struct{}{}
	}
	for _, jail := range settings.EnabledJails {
		if _, allowed := knownJails[jail]; !allowed {
			return nil, nil, model.NewValidationError("unsupported fail2ban jail: " + jail)
		}
		jailSet[jail] = struct{}{}
	}
	jails := make([]string, 0, len(jailSet))
	for jail := range jailSet {
		jails = append(jails, jail)
	}
	sort.Strings(jails)
	return ignoreIPs, jails, nil
}
