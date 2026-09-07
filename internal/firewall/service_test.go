package firewall

import (
	"context"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const sampleStatus = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere                   # SSH
[ 2] 80/tcp                     ALLOW IN    Anywhere                   # HTTP
[ 3] 443/tcp                    ALLOW IN    Anywhere                   # HTTPS
[ 4] 8080/tcp                   DENY IN     192.168.1.0/24             # Block LAN
[ 5] 3306/tcp                   DENY IN     Anywhere                   # MySQL
[ 6] 22/tcp (v6)                ALLOW IN    Anywhere (v6)              # SSH
`

func TestParseStatus(t *testing.T) {
	status := parseUFWStatus(sampleStatus)

	if !status.Active {
		t.Fatal("expected active status")
	}

	if !strings.Contains(status.Default, "deny") {
		t.Fatalf("expected default to contain 'deny', got %q", status.Default)
	}

	if len(status.Rules) != 6 {
		t.Fatalf("expected 6 rules, got %d", len(status.Rules))
	}

	// Verify first rule.
	r1 := status.Rules[0]
	if r1.Number != 1 {
		t.Errorf("rule 1 number: want 1, got %d", r1.Number)
	}
	if r1.To != "22/tcp" {
		t.Errorf("rule 1 to: want 22/tcp, got %q", r1.To)
	}
	if r1.Action != "ALLOW IN" {
		t.Errorf("rule 1 action: want ALLOW IN, got %q", r1.Action)
	}
	if r1.From != "Anywhere" {
		t.Errorf("rule 1 from: want Anywhere, got %q", r1.From)
	}
	if r1.Comment != "SSH" {
		t.Errorf("rule 1 comment: want SSH, got %q", r1.Comment)
	}

	// Verify rule 4 has a CIDR from address.
	r4 := status.Rules[3]
	if r4.Number != 4 {
		t.Errorf("rule 4 number: want 4, got %d", r4.Number)
	}
	if r4.Action != "DENY IN" {
		t.Errorf("rule 4 action: want DENY IN, got %q", r4.Action)
	}
	if r4.From != "192.168.1.0/24" {
		t.Errorf("rule 4 from: want 192.168.1.0/24, got %q", r4.From)
	}
}

func TestParseStatus_Inactive(t *testing.T) {
	output := `Status: inactive`
	status := parseUFWStatus(output)
	if status.Active {
		t.Fatal("expected inactive status")
	}
}

func TestIsSSHRule(t *testing.T) {
	tests := []struct {
		to   string
		want bool
	}{
		{"22/tcp", true},
		{"22", true},
		{"80/tcp", false},
		{"2222/tcp", false},
		{"22/udp", true},
	}
	for _, tt := range tests {
		got := isSSHRule(tt.to)
		if got != tt.want {
			t.Errorf("isSSHRule(%q) = %v, want %v", tt.to, got, tt.want)
		}
	}
}

func TestAddRule_BuildCommand(t *testing.T) {
	var capturedArgs []string

	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			capturedArgs = append([]string{name}, args...)
			return &executor.Result{Stdout: "Rule added", ExitCode: 0}, nil
		},
	}

	svc := NewService(mock, nil)

	// Test basic allow rule with protocol.
	err := svc.AddRule(context.Background(), AddRuleRequest{
		Port:     80,
		Protocol: "tcp",
		Action:   "allow",
		Comment:  "HTTP",
	})
	if err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	expected := []string{"ufw", "allow", "80/tcp", "comment", "HTTP"}
	if len(capturedArgs) != len(expected) {
		t.Fatalf("args length: want %d, got %d: %v", len(expected), len(capturedArgs), capturedArgs)
	}
	for i, v := range expected {
		if capturedArgs[i] != v {
			t.Errorf("arg[%d]: want %q, got %q", i, v, capturedArgs[i])
		}
	}

	// Test deny rule with From address.
	capturedArgs = nil
	err = svc.AddRule(context.Background(), AddRuleRequest{
		Port:   22,
		Action: "deny",
		From:   "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("AddRule with From failed: %v", err)
	}

	expected = []string{"ufw", "deny", "from", "1.2.3.4", "to", "any", "port", "22"}
	if len(capturedArgs) != len(expected) {
		t.Fatalf("args length: want %d, got %d: %v", len(expected), len(capturedArgs), capturedArgs)
	}
	for i, v := range expected {
		if capturedArgs[i] != v {
			t.Errorf("arg[%d]: want %q, got %q", i, v, capturedArgs[i])
		}
	}
}

func TestDeleteRule_SSHWarning(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// Check if this is a status call or a delete call.
			if len(args) > 0 && args[0] == "status" {
				return &executor.Result{Stdout: sampleStatus, ExitCode: 0}, nil
			}
			// Delete call.
			return &executor.Result{Stdout: "Rule deleted", ExitCode: 0}, nil
		},
	}

	svc := NewService(mock, nil)

	// Delete rule 1 (SSH, port 22) without force should error.
	err := svc.DeleteRule(context.Background(), 1, false)
	if err == nil {
		t.Fatal("expected SSH_WARNING error, got nil")
	}
	domainErr, ok := err.(*model.DomainError)
	if !ok {
		t.Fatalf("expected *model.DomainError, got %T", err)
	}
	if domainErr.Code != "SSH_WARNING" {
		t.Errorf("expected code SSH_WARNING, got %q", domainErr.Code)
	}

	// Delete rule 1 with force should succeed.
	err = svc.DeleteRule(context.Background(), 1, true)
	if err != nil {
		t.Fatalf("expected no error with force, got %v", err)
	}

	// Delete rule 2 (HTTP, port 80) without force should succeed.
	err = svc.DeleteRule(context.Background(), 2, false)
	if err != nil {
		t.Fatalf("expected no error for non-SSH rule, got %v", err)
	}
}
