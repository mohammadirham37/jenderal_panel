package noderuntime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshInstallerPreparesPanelUserBeforeBuildAndUsesNVM(t *testing.T) {
	source, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Load definitions only; all host-mutating steps are replaced below.
	definitions := strings.TrimSuffix(strings.TrimSpace(string(source)), `main "$@"`)
	if definitions == strings.TrimSpace(string(source)) {
		t.Fatal("installer entry point not found")
	}
	harness := `
original_build_definition=$(declare -f step_build)
log() { :; }; warn() { :; }
exec 3>&1
apt-get() { { printf 'APT'; printf ' <%s>' "$@"; printf '\n'; } >&3; }
sed() { :; }; systemctl() { return 0; }
curl() { { printf 'CURL'; printf ' <%s>' "$@"; printf '\n'; } >&3; }
step_install_deps
curl() { echo 127.0.0.1; }
preflight() { :; }; setup_interactive() { :; }
step_fix_dpkg() { :; }; step_install_deps() { :; }; step_install_go() { :; }
step_create_user() { panel_user_ready=yes; }
step_create_dirs() { [ "$panel_user_ready" = yes ] || exit 41; panel_dirs_ready=yes; }
step_build() { [ "$panel_user_ready" = yes ] && [ "$panel_dirs_ready" = yes ] || exit 42; echo ORDER_OK; }
step_tls() { :; }; step_config() { :; }; step_sudoers() { :; }; step_migrate() { :; }
step_admin() { :; }; step_systemd() { :; }; step_firewall() { :; }
main --force
sudo() { { printf 'CALL'; printf ' <%s>' "$@"; printf '\n'; } >&3; }
git() { :; }; chown() { :; }; chmod() { :; }; cp() { :; }; rm() { :; }
mkdir() { :; }; install() { :; }; go() { :; }
npm() { echo AMBIENT_NPM >&3; return 99; }
cd() { if [[ "$1" = */web ]]; then builtin cd "$PANEL_FIXTURE/web"; else builtin cd "$PANEL_FIXTURE"; fi; }
JENDERAL_BIN="$PANEL_FIXTURE/fake-panel"
eval "$original_build_definition"
step_build
`
	fixture := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture, "web", "build"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "fake-panel"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash")
	cmd.Env = append(os.Environ(), "PANEL_FIXTURE="+fixture)
	cmd.Stdin = strings.NewReader(definitions + harness)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("installer harness: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "nodesource") || strings.Contains(string(output), "<nodejs>") || strings.Contains(string(output), "AMBIENT_NPM") {
		t.Fatalf("fresh installer attempted global Node installation:\n%s", output)
	}
	for _, want := range []string{"ORDER_OK", "<-u> <jenderal> <-->", "<HOME=/var/lib/jenderal>", "<NODE_VERSION=24>", "/internal/noderuntime/install.sh> <jenderal> <24> </var/lib/jenderal>", "</var/lib/jenderal/.nvm/nvm-exec> <npm> <install> <--loglevel=error>", "</var/lib/jenderal/.nvm/nvm-exec> <npm> <run> <build>"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("missing %s in installer execution:\n%s", want, output)
		}
	}
}

func TestPanelBuildRuntimeIsSeparateAndPinned(t *testing.T) {
	command := PanelNPMCommand("run", "build")
	for _, want := range []string{"/var/lib/jenderal/.nvm/nvm-exec", "HOME=/var/lib/jenderal", "NODE_VERSION=24", "'npm' 'run' 'build'"} {
		if !strings.Contains(command, want) {
			t.Fatalf("panel command missing %s", want)
		}
	}
	bootstrap := PanelBootstrapShell()
	for _, want := range []string{nvmCommit, nvmArchiveSHA256, "/usr/bin/timeout", "'jenderal' '24' '/var/lib/jenderal'"} {
		if !strings.Contains(bootstrap, want) {
			t.Fatalf("bootstrap missing %s", want)
		}
	}
	if strings.Contains(bootstrap, "apt-get") || strings.Contains(command, "/usr/bin/npm") {
		t.Fatal("panel build depends on global Node")
	}
}
