package dbmanager

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The PGDG signing key decides which packages apt will install as root, so
// the downloaded key is checked against its published fingerprint before it
// is trusted — the same rule the PHP module applies to the packages.sury.org key.
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

func TestPGDGRepositorySetupScriptOnlyConfiguresTheRepository(t *testing.T) {
	script := PGDGRepositorySetupScript()
	for _, want := range []string{pgdgKeyFingerprint, "%s-pgdg main", "/etc/apt/sources.list.d/pgdg.list", "lsb_release -cs"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
	if strings.Contains(script, "apt-get install") {
		t.Error("the repository setup must not install packages")
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader("set -eu\n" + script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid bash: %v\n%s", err, out)
	}
}

// pgdg's postgresql meta package always depends on the newest major
// (postgresql-19 on a 26.04 server running 18), and the panel's
// apt upgrade --with-new-pkgs would install it. The meta packages are
// pinned below the Ubuntu archive whenever pgdg is configured.
func TestPGDGMetaPackagesArePinnedBelowUbuntu(t *testing.T) {
	if !strings.Contains(PGDGRepositorySetupScript(), PGDGPinPath) {
		t.Error("the pgdg setup must write the meta package pin")
	}
	root := t.TempDir()
	script := strings.ReplaceAll(PGDGMetaPinScript(), "/etc/apt/", root+"/etc/apt/")
	run := func() {
		t.Helper()
		if out, err := exec.Command("bash", "-c", "set -eu\n"+script).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}
	os.MkdirAll(filepath.Join(root, "etc/apt/sources.list.d"), 0o755)
	run()
	pin := filepath.Join(root, strings.TrimPrefix(PGDGPinPath, "/"))
	if _, err := os.Stat(pin); err == nil {
		t.Fatal("pin written although pgdg is not configured")
	}
	os.WriteFile(filepath.Join(root, "etc/apt/sources.list.d/pgdg.sources"),
		[]byte("Types: deb\nURIs: https://apt.postgresql.org/pub/repos/apt\nSuites: resolute-pgdg\n"), 0o644)
	run()
	got, err := os.ReadFile(pin)
	if err != nil {
		t.Fatal("pin not written with pgdg configured")
	}
	for _, want := range []string{"Package: postgresql postgresql-client postgresql-contrib postgresql-doc", "Pin: origin apt.postgresql.org", "Pin-Priority: 100"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("pin missing %q:\n%s", want, got)
		}
	}
}
