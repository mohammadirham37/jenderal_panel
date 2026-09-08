package main

import (
	"runtime/debug"
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
