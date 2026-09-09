package main

import (
	"context"
	"errors"
	"runtime/debug"
	"strings"
	"testing"
)

func TestResolveVersionUsesBinaryVCSRevision(t *testing.T) {
	const revision = "1234567890abcdef1234567890abcdef12345678"
	settings := []debug.BuildSetting{
		{Key: "vcs.revision", Value: revision},
		{Key: "vcs.modified", Value: "false"},
	}

	if got := resolveVersion("0.1.0", settings); got != revision {
		t.Fatalf("resolveVersion() = %q, want running VCS revision %q", got, revision)
	}
}

func TestResolveVersionFallsBackWithoutVCSRevision(t *testing.T) {
	if got := resolveVersion("0.1.0", nil); got != "0.1.0" {
		t.Fatalf("resolveVersion() = %q, want linked version", got)
	}
}

func TestRestartServiceRestartsAndVerifiesSystemdUnit(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if len(calls) == 2 {
			return []byte("active\n"), nil
		}
		return nil, nil
	}

	if err := restartService(0, run); err != nil {
		t.Fatalf("restartService() error = %v", err)
	}

	want := [][]string{
		{"/usr/bin/systemctl", "restart", "jenderal.service"},
		{"/usr/bin/systemctl", "is-active", "jenderal.service"},
	}
	if len(calls) != len(want) {
		t.Fatalf("command calls = %v, want %v", calls, want)
	}
	for i := range want {
		if strings.Join(calls[i], "\x00") != strings.Join(want[i], "\x00") {
			t.Fatalf("command call %d = %v, want %v", i, calls[i], want[i])
		}
	}
}

func TestRestartServiceRequiresRootWithoutRunningCommand(t *testing.T) {
	called := false
	err := restartService(1000, func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return nil, nil
	})

	if err == nil || !strings.Contains(err.Error(), "sudo /opt/jenderal/jenderal restart") {
		t.Fatalf("restartService() error = %v, want sudo guidance", err)
	}
	if called {
		t.Fatal("restartService() ran systemctl without root")
	}
}

func TestRestartServiceReportsSystemctlFailure(t *testing.T) {
	err := restartService(0, func(context.Context, string, ...string) ([]byte, error) {
		return []byte("unit failed"), errors.New("exit status 1")
	})

	if err == nil || !strings.Contains(err.Error(), "unit failed") {
		t.Fatalf("restartService() error = %v, want systemctl output", err)
	}
}

func TestRestartServiceRejectsInactiveUnit(t *testing.T) {
	calls := 0
	err := restartService(0, func(context.Context, string, ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, nil
		}
		return []byte("inactive\n"), errors.New("exit status 3")
	})

	if err == nil || !strings.Contains(err.Error(), "inactive") || !strings.Contains(err.Error(), "journalctl") {
		t.Fatalf("restartService() error = %v, want inactive status and log guidance", err)
	}
}
