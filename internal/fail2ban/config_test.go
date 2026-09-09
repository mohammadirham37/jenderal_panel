package fail2ban

import (
	"strings"
	"testing"
)

func TestRenderSafeConfigUsesTemporarySSHBanAndLiteralCIDRs(t *testing.T) {
	got, err := Render(Settings{
		SSHDEnabled: true, MaxRetry: 5, FindTimeSeconds: 600, BanTimeSeconds: 900,
		IgnoreIPs: []string{"127.0.0.1/8", "203.0.113.8/32"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[sshd]", "maxretry = 5", "findtime = 600", "bantime = 900",
		"ignoreip = 127.0.0.1/8 203.0.113.8/32",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestRenderRejectsPermanentOrInvalidNetworkSettings(t *testing.T) {
	settings := SafeSettings()
	settings.BanTimeSeconds = 0
	if _, err := Render(settings); err == nil {
		t.Fatal("permanent ban duration accepted")
	}
	settings = SafeSettings()
	settings.IgnoreIPs = []string{"203.0.113.8; rm -rf /"}
	if _, err := Render(settings); err == nil {
		t.Fatal("invalid ignore address accepted")
	}
}

func TestManagedSettingsRoundTrip(t *testing.T) {
	want := SafeSettings()
	want.IgnoreIPs = append(want.IgnoreIPs, "203.0.113.8/32")
	want.EnabledJails = []string{"nginx-http-auth"}
	content, err := Render(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseSettings(content)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SSHDEnabled || got.MaxRetry != 5 || got.BanTimeSeconds != 900 || len(got.EnabledJails) != 1 {
		t.Fatalf("settings = %#v", got)
	}
	if !strings.Contains(strings.Join(got.IgnoreIPs, " "), "203.0.113.8/32") {
		t.Fatalf("ignore IPs = %#v", got.IgnoreIPs)
	}
}
