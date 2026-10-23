package dbmanager

import (
	"os/exec"
	"strings"
	"testing"
)

// The PGDG signing key decides which packages apt will install as root, so
// the downloaded key is checked against its published fingerprint before it
// is trusted — the same rule the PHP module applies to the ondrej key.
func TestPgvectorInstallScriptVerifiesTheRepositoryKey(t *testing.T) {
	script := pgvectorInstallScript("16")

	if syntax := exec.Command("bash", "-n"); syntax != nil {
		cmd := exec.Command("bash", "-n")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("install script is not valid bash: %v: %s", err, out)
		}
	}
	if !strings.Contains(script, pgdgKeyFingerprint) {
		t.Errorf("script does not pin the PGDG key fingerprint:\n%s", script)
	}
	for _, want := range []string{"gpg --batch --show-keys", "exit 1"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing key verification step %q:\n%s", want, script)
		}
	}
	// The key must be verified before the repository is enabled.
	if strings.Index(script, pgdgKeyFingerprint) > strings.Index(script, "sources.list.d/pgdg.list") {
		t.Error("the key is trusted before it is verified")
	}
}
