package cloudflared

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

func validToken() string {
	b, _ := json.Marshal(tokenPayload{A: "acc", T: "tunnel-id", S: "secret"})
	return base64.StdEncoding.EncodeToString(b)
}

func TestValidateTokenAcceptsConnectorToken(t *testing.T) {
	if err := ValidateToken(validToken()); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateTokenAcceptsUnpaddedBase64(t *testing.T) {
	token := strings.TrimRight(validToken(), "=")
	if err := ValidateToken(token); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateTokenRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty":          "   ",
		"not base64":     "!!! not base64 !!!",
		"not json":       base64.StdEncoding.EncodeToString([]byte("hello world")),
		"missing secret": base64.StdEncoding.EncodeToString([]byte(`{"a":"acc"}`)),
	}
	for name, token := range cases {
		if err := ValidateToken(token); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestRenderUnitUsesEnvFileAndPinnedBinary(t *testing.T) {
	unit := RenderUnit()
	for _, want := range []string{
		"EnvironmentFile=" + TokenEnvPath,
		"ExecStart=" + BinaryPath + " tunnel --no-autoupdate run",
		"Restart=always",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
}

func TestInstallScriptPinsChecksumsPerArch(t *testing.T) {
	script := installScript()
	if !strings.Contains(script, "cloudflared/releases/download/"+Version+"/cloudflared-linux-") {
		t.Fatalf("script missing pinned release URL:\n%s", script)
	}
	for arch, sum := range checksums {
		if !strings.Contains(script, arch) || !strings.Contains(script, sum) {
			t.Fatalf("script missing arch %s or checksum:\n%s", arch, script)
		}
	}
	if !strings.Contains(script, "unsupported architecture") {
		t.Fatalf("script missing unsupported arch guard:\n%s", script)
	}
}

func TestParseVersionReadsCloudflaredOutput(t *testing.T) {
	out := "cloudflared version 2026.9.1 (built 2026-09-11-13:35 UTC)\n"
	if got := parseVersion(out); got != Version {
		t.Fatalf("got %q want %q", got, Version)
	}
	if got := parseVersion("no version here"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestStatusReadsBinaryTokenAndService(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: "cloudflared version " + Version + "\n"}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch {
			case name == "systemctl" && args[len(args)-1] == UnitName:
				return &executor.Result{ExitCode: 0, Stdout: "active\n"}, nil
			case name == "systemctl":
				return &executor.Result{ExitCode: 3, Stdout: "inactive\n"}, nil
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	s := NewService(mock, nil)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Installed || st.Version != Version || !st.TokenInstalled {
		t.Fatalf("bad status: %+v", st)
	}
	if st.ServiceState != "active" {
		t.Fatalf("service state %q, want active", st.ServiceState)
	}
	if st.AptServiceRunning {
		t.Fatal("apt cloudflared service should be inactive")
	}
}

func TestStatusReportsMissingBinary(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 127}, errors.New("exec: not found")
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 4, Stdout: "inactive\n"}, nil
		},
	}
	s := NewService(mock, nil)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatalf("missing binary must not error: %v", err)
	}
	if st.Installed || st.TokenInstalled || st.ServiceState != "inactive" {
		t.Fatalf("bad status: %+v", st)
	}
}

func TestConnectWritesTokenUnitAndRestarts(t *testing.T) {
	var teeInput, heredoc string
	var systemctlCmds [][]string
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{ExitCode: 0, Stdout: "cloudflared version " + Version + "\n"}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch {
			case name == "systemctl":
				systemctlCmds = append(systemctlCmds, args)
			case name == "bash":
				heredoc = args[1]
			}
			return &executor.Result{ExitCode: 0}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			if name == "tee" {
				teeInput = input
			}
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	s := NewService(mock, nil)
	if err := s.Connect(context.Background(), validToken(), func(string) {}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(teeInput, "TUNNEL_TOKEN=") {
		t.Fatalf("env file missing token: %q", teeInput)
	}
	if !strings.Contains(heredoc, "ExecStart="+BinaryPath+" tunnel --no-autoupdate run") {
		t.Fatalf("unit not written via heredoc: %q", heredoc)
	}
	restarted := false
	for _, args := range systemctlCmds {
		if len(args) >= 2 && args[0] == "restart" && args[1] == UnitName {
			restarted = true
		}
	}
	if !restarted {
		t.Fatalf("service not restarted: %v", systemctlCmds)
	}
}

func TestConnectRejectsInvalidTokenBeforeRunningCommands(t *testing.T) {
	called := false
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			called = true
			return &executor.Result{}, nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			called = true
			return &executor.Result{}, nil
		},
		RunSudoWithInputFunc: func(ctx context.Context, input, name string, args ...string) (*executor.Result, error) {
			called = true
			return &executor.Result{}, nil
		},
	}
	s := NewService(mock, nil)
	if err := s.Connect(context.Background(), "garbage", func(string) {}); err == nil {
		t.Fatal("expected validation error")
	}
	if called {
		t.Fatal("no command may run for an invalid token")
	}
}

func TestDisconnectStopsAndRemovesAssets(t *testing.T) {
	var cmds [][]string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			cmds = append(cmds, append([]string{name}, args...))
			return &executor.Result{ExitCode: 0}, nil
		},
	}
	s := NewService(mock, nil)
	if err := s.Disconnect(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := fmt.Sprint(cmds)
	for _, want := range []string{"stop", "disable", UnitName, TokenEnvPath, "daemon-reload"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("disconnect missing %s: %v", want, cmds)
		}
	}
}

func TestLogsTailsJournalctlWithCap(t *testing.T) {
	var args []string
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, argsc ...string) (*executor.Result, error) {
			args = append([]string{name}, argsc...)
			return &executor.Result{ExitCode: 0, Stdout: "log line\n"}, nil
		},
	}
	s := NewService(mock, nil)
	out, err := s.Logs(context.Background(), 50)
	if err != nil || out != "log line\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := fmt.Sprint(args); !strings.Contains(got, UnitName) || !strings.Contains(got, "50") || !strings.Contains(got, "--no-pager") {
		t.Fatalf("bad journalctl args: %v", args)
	}
	if _, err := s.Logs(context.Background(), 99999); err != nil {
		t.Fatalf("oversized lines must be capped, not error: %v", err)
	}
}
