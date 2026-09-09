package fail2ban

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func ParseActiveJails(output string) []string {
	set := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		for _, candidate := range strings.Split(line[colon+1:], ",") {
			jail := strings.TrimSpace(candidate)
			if _, allowed := knownJails[jail]; allowed {
				set[jail] = struct{}{}
			}
		}
	}
	jails := make([]string, 0, len(set))
	for jail := range set {
		jails = append(jails, jail)
	}
	sort.Strings(jails)
	return jails
}

func ParseJailStatus(name, output string) (Jail, error) {
	jail := Jail{Name: name, BanTimeSeconds: SafeSettings().BanTimeSeconds, BannedIPs: []string{}}
	foundCurrentBanned := false
	foundTotalBanned := false
	for _, line := range strings.Split(output, "\n") {
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		label := normalizeStatusLabel(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		var target *int
		switch label {
		case "currently failed":
			target = &jail.CurrentlyFailed
		case "total failed":
			target = &jail.TotalFailed
		case "currently banned":
			target = &jail.CurrentlyBanned
			foundCurrentBanned = true
		case "total banned":
			target = &jail.TotalBanned
			foundTotalBanned = true
		case "banned ip list":
			if value != "" {
				jail.BannedIPs = strings.Fields(value)
			}
			continue
		default:
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return Jail{}, fmt.Errorf("parse %s for jail %s: %q", label, name, value)
		}
		*target = parsed
	}
	if !foundCurrentBanned {
		return Jail{}, fmt.Errorf("parse currently banned for jail %s: field missing", name)
	}
	if !foundTotalBanned {
		return Jail{}, fmt.Errorf("parse total banned for jail %s: field missing", name)
	}
	return jail, nil
}

func normalizeStatusLabel(value string) string {
	value = strings.TrimLeft(value, " |`-+\t")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func parseSystemctlProperties(output string) map[string]string {
	properties := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			properties[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return properties
}

func parseSSHPort(output string) int {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[0], "port") {
			if port, err := strconv.Atoi(fields[1]); err == nil && port >= 1 && port <= 65535 {
				return port
			}
		}
	}
	return 0
}
