package cloudflared

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
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
