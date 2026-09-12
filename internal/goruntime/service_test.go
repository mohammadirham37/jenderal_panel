package goruntime

import (
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"go version go1.27.1 linux/amd64": "1.27.1",
		"go version devel +abc":           "",
		"":                                "",
	}
	for output, want := range cases {
		if got := parseVersion(output); got != want {
			t.Errorf("parseVersion(%q) = %q, want %q", output, got, want)
		}
	}
}

func TestInstallScriptPinnedAndSafe(t *testing.T) {
	script := installScript()
	for _, want := range []string{
		"sha256sum -c",
		Version,
		GoBin,
		"unsupported architecture",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install script missing %q", want)
		}
	}
	// No user input exists, but confirm no obvious injection surface.
	if strings.Contains(script, "eval") {
		t.Error("install script must not use eval")
	}
}
