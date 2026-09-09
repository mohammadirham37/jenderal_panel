package security

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func TestPostureReportsAppArmorMixedModesWithoutCallingItDisabled(t *testing.T) {
	exec := postureFixture(t, map[string]*executor.Result{
		"ufw status verbose":                   {Stdout: "Status: active\n"},
		"aa-status --json":                     {Stdout: `{"profiles":{"enforce":10,"complain":2},"processes":{"unconfined":1}}`},
		"/usr/sbin/sshd -T":                    {Stdout: "port 22\npermitrootlogin no\npasswordauthentication no\n"},
		"/usr/sbin/nginx -t":                   {},
		"ubuntu-security-status --format json": {Stdout: `{"security_updates":0}`},
	})
	report := NewPostureChecker(exec).Check(context.Background(), time.Now())
	f := findingCode(report, "apparmor_profiles_not_enforced")
	if f == nil || f.Severity != SeverityMedium || !strings.Contains(f.Summary, "2") {
		t.Fatalf("finding=%#v", f)
	}
}

func TestPostureTreatsUnavailableSecurityUpdateToolAsUnknown(t *testing.T) {
	exec := postureFixture(t, map[string]*executor.Result{})
	report := NewPostureChecker(exec).Check(context.Background(), time.Now())
	if report.Components["security_updates"] != "unknown" {
		t.Fatalf("state=%q", report.Components["security_updates"])
	}
}

func TestAppArmorParserSupportsProfileArraysAndTextFallback(t *testing.T) {
	if count, ok := appArmorModeCount([]byte(`["a","b"]`)); !ok || count != 2 {
		t.Fatalf("array count=%d ok=%v", count, ok)
	}
	enforced, complain, ok := parseAppArmorText("10 profiles are in enforce mode.\n2 profiles are in complain mode.\n")
	if !ok || enforced != 10 || complain != 2 {
		t.Fatalf("enforced=%d complain=%d ok=%v", enforced, complain, ok)
	}
}

func postureFixture(t *testing.T, results map[string]*executor.Result) *executor.MockExecutor {
	run := func(_ context.Context, name string, args ...string) (*executor.Result, error) {
		key := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if result, ok := results[key]; ok {
			copy := *result
			return &copy, nil
		}
		return &executor.Result{ExitCode: 127, Stderr: "command not found"}, nil
	}
	return &executor.MockExecutor{RunFunc: run, RunSudoFunc: run, RunSudoWithInputFunc: func(context.Context, string, string, ...string) (*executor.Result, error) {
		t.Fatal("posture probe attempted a write")
		return nil, nil
	}}
}

func findingCode(report PostureReport, code string) *Finding {
	for i := range report.Findings {
		if report.Findings[i].Code == code {
			return &report.Findings[i]
		}
	}
	return nil
}
