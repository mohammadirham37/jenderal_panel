package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

const maxPostureOutput = 1 << 20

type Finding struct {
	Code        string         `json:"code"`
	Component   string         `json:"component"`
	Severity    Severity       `json:"severity"`
	State       string         `json:"state"`
	Summary     string         `json:"summary"`
	Remediation string         `json:"remediation"`
	Evidence    map[string]any `json:"evidence,omitempty"`
}

type PostureReport struct {
	CheckedAt  time.Time         `json:"checked_at"`
	Components map[string]string `json:"components"`
	Findings   []Finding         `json:"findings"`
}

type PostureChecker struct{ exec executor.CommandExecutor }

func NewPostureChecker(exec executor.CommandExecutor) *PostureChecker {
	return &PostureChecker{exec: exec}
}

func (p *PostureChecker) Check(ctx context.Context, now time.Time) PostureReport {
	report := PostureReport{CheckedAt: now.UTC(), Components: map[string]string{}, Findings: []Finding{}}
	p.checkUFW(ctx, &report)
	p.checkAppArmor(ctx, &report)
	p.checkSSH(ctx, &report)
	p.checkNginx(ctx, &report)
	p.checkSecurityUpdates(ctx, &report)
	return report
}

func (p *PostureChecker) command(ctx context.Context, name string, args ...string) (*executor.Result, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := p.exec.RunSudo(probeCtx, name, args...)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("probe returned no result")
	}
	if len(result.Stdout)+len(result.Stderr) > maxPostureOutput {
		return nil, fmt.Errorf("probe output exceeded 1 MiB")
	}
	return result, nil
}

func (p *PostureChecker) checkUFW(ctx context.Context, report *PostureReport) {
	r, err := p.command(ctx, "ufw", "status", "verbose")
	if err != nil || r.ExitCode != 0 {
		report.Components["firewall"] = "unknown"
		return
	}
	if strings.Contains(strings.ToLower(r.Stdout), "status: active") {
		report.Components["firewall"] = "healthy"
		return
	}
	if !strings.Contains(strings.ToLower(r.Stdout), "status: inactive") {
		report.Components["firewall"] = "unknown"
		return
	}
	report.Components["firewall"] = "inactive"
	report.Findings = append(report.Findings, Finding{Code: "ufw_inactive", Component: "firewall", Severity: SeverityMedium, State: "inactive", Summary: "UFW firewall is inactive", Remediation: "Review required ports and enable UFW from the Firewall page without closing provider-console access."})
}

func (p *PostureChecker) checkAppArmor(ctx context.Context, report *PostureReport) {
	r, err := p.command(ctx, "aa-status", "--json")
	enforced, complain, parsed := 0, 0, false
	if err == nil && r.ExitCode == 0 {
		var value struct {
			Profiles map[string]json.RawMessage `json:"profiles"`
		}
		if json.Unmarshal([]byte(r.Stdout), &value) == nil && value.Profiles != nil {
			enforced, _ = appArmorModeCount(value.Profiles["enforce"])
			complain, _ = appArmorModeCount(value.Profiles["complain"])
			parsed = true
		}
	}
	if !parsed {
		text, textErr := p.command(ctx, "aa-status")
		if textErr == nil && text.ExitCode == 0 {
			enforced, complain, parsed = parseAppArmorText(text.Stdout)
		}
	}
	if !parsed {
		report.Components["apparmor"] = "unknown"
		return
	}
	if complain > 0 {
		report.Components["apparmor"] = "mixed"
		report.Findings = append(report.Findings, Finding{Code: "apparmor_profiles_not_enforced", Component: "apparmor", Severity: SeverityMedium, State: "mixed", Summary: fmt.Sprintf("%d AppArmor profile(s) are in complain mode", complain), Remediation: "Review the affected profiles before changing their mode.", Evidence: map[string]any{"enforced": enforced, "complain": complain}})
		return
	}
	if enforced == 0 {
		report.Components["apparmor"] = "unknown"
		return
	}
	report.Components["apparmor"] = "healthy"
}

func appArmorModeCount(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var count int
	if json.Unmarshal(raw, &count) == nil {
		return count, count >= 0
	}
	var profiles []any
	if json.Unmarshal(raw, &profiles) == nil {
		return len(profiles), true
	}
	return 0, false
}

func parseAppArmorText(output string) (int, int, bool) {
	enforced, complain, matched := 0, 0, false
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		count, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "profiles are in enforce mode") || strings.Contains(lower, "profile is in enforce mode") {
			enforced, matched = count, true
		}
		if strings.Contains(lower, "profiles are in complain mode") || strings.Contains(lower, "profile is in complain mode") {
			complain, matched = count, true
		}
	}
	return enforced, complain, matched
}

func (p *PostureChecker) checkSSH(ctx context.Context, report *PostureReport) {
	r, err := p.command(ctx, "/usr/sbin/sshd", "-T")
	if err != nil || r.ExitCode != 0 {
		report.Components["ssh"] = "unknown"
		return
	}
	settings := map[string]string{}
	for _, line := range strings.Split(r.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			settings[strings.ToLower(fields[0])] = strings.ToLower(fields[1])
		}
	}
	if len(settings) == 0 {
		report.Components["ssh"] = "unknown"
		return
	}
	report.Components["ssh"] = "healthy"
	if settings["permitrootlogin"] == "yes" {
		report.Components["ssh"] = "needs_attention"
		report.Findings = append(report.Findings, Finding{Code: "ssh_root_login_enabled", Component: "ssh", Severity: SeverityMedium, State: "enabled", Summary: "SSH root login is permitted", Remediation: "Confirm a tested sudo account and provider-console access before changing SSH settings."})
	}
	if settings["passwordauthentication"] == "yes" {
		report.Findings = append(report.Findings, Finding{Code: "ssh_password_auth_enabled", Component: "ssh", Severity: SeverityLow, State: "enabled", Summary: "SSH password authentication is enabled", Remediation: "Prefer tested SSH keys before disabling password authentication."})
	}
	if port, err := strconv.Atoi(settings["port"]); err == nil {
		if len(report.Findings) > 0 {
			report.Findings[len(report.Findings)-1].Evidence = map[string]any{"port": port}
		}
	}
}

func (p *PostureChecker) checkNginx(ctx context.Context, report *PostureReport) {
	r, err := p.command(ctx, "/usr/sbin/nginx", "-t")
	if err != nil {
		report.Components["nginx"] = "unknown"
		return
	}
	if r.ExitCode == 0 {
		report.Components["nginx"] = "healthy"
		return
	}
	report.Components["nginx"] = "invalid"
	report.Findings = append(report.Findings, Finding{Code: "nginx_invalid", Component: "nginx", Severity: SeverityCritical, State: "invalid", Summary: "Nginx configuration is invalid", Remediation: "Review the Nginx test output and restore the last known-good configuration."})
}

func (p *PostureChecker) checkSecurityUpdates(ctx context.Context, report *PostureReport) {
	r, err := p.command(ctx, "ubuntu-security-status", "--format", "json")
	if err != nil || r.ExitCode != 0 {
		report.Components["security_updates"] = "unknown"
		return
	}
	var value map[string]any
	if json.Unmarshal([]byte(r.Stdout), &value) != nil {
		report.Components["security_updates"] = "unknown"
		return
	}
	rawCount, exists := value["security_updates"]
	count, valid := numberFromJSON(rawCount)
	if !exists || !valid {
		report.Components["security_updates"] = "unknown"
		return
	}
	if count > 0 {
		report.Components["security_updates"] = "needs_attention"
		report.Findings = append(report.Findings, Finding{Code: "security_updates_available", Component: "security_updates", Severity: SeverityMedium, State: "available", Summary: fmt.Sprintf("%d security update(s) are available", count), Remediation: "Review and apply Ubuntu security updates during a maintenance window.", Evidence: map[string]any{"count": count}})
		return
	}
	report.Components["security_updates"] = "healthy"
}

func numberFromJSON(v any) (int, bool) {
	switch value := v.(type) {
	case float64:
		return int(value), value >= 0
	case json.Number:
		n, _ := value.Int64()
		return int(n), n >= 0
	default:
		return 0, false
	}
}
