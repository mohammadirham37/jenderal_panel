package firewall

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// AddRuleRequest holds the parameters for adding a new firewall rule.
type AddRuleRequest struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // tcp, udp, or both
	Action   string `json:"action"`   // allow, deny, or limit
	From     string `json:"from"`
	Comment  string `json:"comment"`
}

// Service manages UFW firewall operations.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new firewall Service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// Status returns the current UFW firewall status.
func (s *Service) Status(ctx context.Context) (*model.FirewallStatus, error) {
	result, err := s.exec.RunSudo(ctx, "ufw", "status", "numbered", "verbose")
	if err != nil {
		return nil, fmt.Errorf("ufw status: %w", err)
	}
	return parseUFWStatus(result.Stdout), nil
}

// Enable activates the UFW firewall.
func (s *Service) Enable(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "ufw", "--force", "enable")
	if err != nil {
		return fmt.Errorf("ufw enable: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("UFW_ERROR", "failed to enable firewall: "+result.Stderr, nil)
	}
	return nil
}

// Disable deactivates the UFW firewall.
func (s *Service) Disable(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "ufw", "disable")
	if err != nil {
		return fmt.Errorf("ufw disable: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("UFW_ERROR", "failed to disable firewall: "+result.Stderr, nil)
	}
	return nil
}

// AddRule adds a new UFW firewall rule.
func (s *Service) AddRule(ctx context.Context, req AddRuleRequest) error {
	if req.Port < 1 || req.Port > 65535 {
		return model.NewValidationError("port must be between 1 and 65535")
	}

	action := strings.ToLower(req.Action)
	if action == "" {
		action = "allow"
	}
	if action != "allow" && action != "deny" && action != "limit" {
		return model.NewValidationError("action must be allow, deny, or limit")
	}

	protocol := strings.ToLower(req.Protocol)
	if protocol != "" && protocol != "tcp" && protocol != "udp" && protocol != "both" {
		return model.NewValidationError("protocol must be tcp, udp, or both")
	}

	var args []string

	if req.From != "" {
		// ufw allow/deny from <addr> to any port <port>
		args = append(args, action, "from", req.From, "to", "any", "port", strconv.Itoa(req.Port))
		if protocol != "" && protocol != "both" {
			args = append(args, "proto", protocol)
		}
	} else {
		// ufw allow/deny <port>[/proto]
		portStr := strconv.Itoa(req.Port)
		if protocol != "" && protocol != "both" {
			portStr += "/" + protocol
		}
		args = append(args, action, portStr)
		if req.Comment != "" {
			args = append(args, "comment", req.Comment)
		}
	}

	result, err := s.exec.RunSudo(ctx, "ufw", args...)
	if err != nil {
		return fmt.Errorf("ufw add rule: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("UFW_ERROR", "failed to add rule: "+result.Stderr, nil)
	}
	return nil
}

// DeleteRule removes a firewall rule by its number. If force is false and
// the rule protects SSH, a domain error with code "SSH_WARNING" is returned.
func (s *Service) DeleteRule(ctx context.Context, number int, force bool) error {
	if !force {
		status, err := s.Status(ctx)
		if err != nil {
			return err
		}
		for _, rule := range status.Rules {
			if rule.Number == number {
				if isSSHRule(rule.To) {
					return model.NewDomainError("SSH_WARNING",
						"this rule appears to protect SSH access; set force=true to delete", nil)
				}
				break
			}
		}
	}

	result, err := s.exec.RunSudo(ctx, "ufw", "--force", "delete", strconv.Itoa(number))
	if err != nil {
		return fmt.Errorf("ufw delete rule: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("UFW_ERROR", "failed to delete rule: "+result.Stderr, nil)
	}
	return nil
}

// isSSHRule returns true if the To field indicates an SSH rule (port 22).
func isSSHRule(to string) bool {
	return strings.HasPrefix(to, "22/") || to == "22"
}

// ruleRe matches a numbered rule line from "ufw status numbered verbose".
var ruleRe = regexp.MustCompile(`\[\s*(\d+)\]\s+(.+?)\s+(ALLOW IN|DENY IN|LIMIT IN|REJECT IN)\s+(.+?)(?:\s+#\s+(.*))?$`)

// parseUFWStatus parses the output of "ufw status numbered verbose" into a
// FirewallStatus model.
func parseUFWStatus(output string) *model.FirewallStatus {
	status := &model.FirewallStatus{}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Determine active/inactive.
		if strings.HasPrefix(trimmed, "Status:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "Status:"))
			status.Active = val == "active"
		}

		// Parse default policy.
		if strings.HasPrefix(trimmed, "Default:") {
			status.Default = strings.TrimSpace(strings.TrimPrefix(trimmed, "Default:"))
		}

		// Parse numbered rules.
		if m := ruleRe.FindStringSubmatch(line); m != nil {
			num, _ := strconv.Atoi(m[1])
			status.Rules = append(status.Rules, model.FirewallRule{
				Number:  num,
				To:      strings.TrimSpace(m[2]),
				Action:  strings.TrimSpace(m[3]),
				From:    strings.TrimSpace(m[4]),
				Comment: strings.TrimSpace(m[5]),
			})
		}
	}

	return status
}
