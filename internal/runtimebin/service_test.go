package runtimebin

import (
	"strings"
	"testing"
)

func TestChecksumsPinnedForEveryArch(t *testing.T) {
	for _, rt := range []string{"deno", "bun"} {
		arts, ok := artifacts[rt]
		if !ok {
			t.Fatalf("artifacts missing for %s", rt)
		}
		sums, ok := checksums[rt]
		if !ok {
			t.Fatalf("checksums missing for %s", rt)
		}
		for arch, art := range arts {
			sum, ok := sums[arch]
			if !ok || len(sum) != 64 {
				t.Errorf("%s/%s: checksum missing or malformed (%q)", rt, arch, sum)
			}
			if art.zipURL == "" || art.binName == "" {
				t.Errorf("%s/%s: incomplete artifact %+v", rt, arch, art)
			}
		}
	}
}

func TestInstallScriptPinnedPerArch(t *testing.T) {
	svc := &Service{}
	script, err := svc.installScript("deno")
	if err != nil {
		t.Fatalf("installScript: %v", err)
	}
	for _, want := range []string{"deno-x86_64-unknown-linux-gnu.zip", "sha256sum -c", DenoBin} {
		if !strings.Contains(script, want) {
			t.Errorf("deno script missing %q", want)
		}
	}

	script, err = svc.installScript("bun")
	if err != nil {
		t.Fatalf("installScript: %v", err)
	}
	if !strings.Contains(script, "bun-linux-x64.zip") || !strings.Contains(script, BunBin) {
		t.Errorf("bun script incomplete")
	}

	if _, err := svc.installScript("go"); err == nil {
		t.Error("unknown runtime must be rejected")
	}
}
