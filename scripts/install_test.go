package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func runInstallSummary(t *testing.T, commandMocks string) string {
	t.Helper()
	source, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	definitions := strings.TrimSuffix(strings.TrimSpace(string(source)), `main "$@"`)
	if definitions == strings.TrimSpace(string(source)) {
		t.Fatal("installer entry point not found")
	}
	script := definitions + `
PANEL_PORT=8443
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=secret
ADMIN_HOSTNAME=panel.example.com
` + commandMocks + `
print_install_summary
`
	cmd := exec.Command("bash")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("print_install_summary failed: %v\n%s", err, output)
	}
	ansiEscape := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiEscape.ReplaceAllString(string(output), "")
}

func TestInstallSummaryUsesBackupPublicIPService(t *testing.T) {
	output := runInstallSummary(t, `
curl() {
  case "$*" in
    *api.ipify.org*) return 22 ;;
    *checkip.amazonaws.com*) printf '45.76.12.34\n' ;;
    *) return 22 ;;
  esac
}
hostname() { printf '10.65.85.254\n'; }
`)

	if !strings.Contains(output, "URL:       https://45.76.12.34:8443") {
		t.Fatalf("summary output does not contain public URL:\n%s", output)
	}
}

func TestInstallSummaryRejectsPrivateAddressAndUsesConfiguredHostname(t *testing.T) {
	output := runInstallSummary(t, `
curl() { printf '10.65.85.254\n'; }
hostname() { printf '10.65.85.254 172.16.0.8 192.168.1.2\n'; }
`)

	if !strings.Contains(output, "URL:       https://panel.example.com:8443") {
		t.Fatalf("summary output does not contain configured hostname URL:\n%s", output)
	}
	if strings.Contains(output, "https://10.65.85.254:8443") {
		t.Fatalf("summary output exposes a private URL:\n%s", output)
	}
}

func TestInstallSummaryUsesPublicInterfaceAddressBeforeHostname(t *testing.T) {
	output := runInstallSummary(t, `
curl() { return 22; }
hostname() {
  if [ "$1" = "-I" ]; then
    printf '10.65.85.254 45.76.99.10\n'
  else
    printf 'system-host.example.com\n'
  fi
}
`)

	if !strings.Contains(output, "URL:       https://45.76.99.10:8443") {
		t.Fatalf("summary output does not contain public interface URL:\n%s", output)
	}
}

func TestInstallSummaryRejectsMalformedAndReservedPublicLookupResults(t *testing.T) {
	output := runInstallSummary(t, `
curl() {
  case "$*" in
    *api.ipify.org*) printf '1.2.3.4.\n' ;;
    *checkip.amazonaws.com*) printf '192.88.99.1\n' ;;
    *) return 22 ;;
  esac
}
hostname() { printf '10.65.85.254\n'; }
`)

	if !strings.Contains(output, "URL:       https://panel.example.com:8443") {
		t.Fatalf("summary output accepted malformed or reserved address:\n%s", output)
	}
}

func TestInstallSummaryDoesNotUsePrivateLiteralAsConfiguredHostname(t *testing.T) {
	output := runInstallSummary(t, `
curl() { return 22; }
hostname() {
  if [ "$1" = "-I" ]; then
    printf '10.65.85.254\n'
  else
    printf 'system-host.example.com\n'
  fi
}
ADMIN_HOSTNAME=10.65.85.254
`)

	if !strings.Contains(output, "URL:       https://system-host.example.com:8443") {
		t.Fatalf("summary output did not reject private configured IP:\n%s", output)
	}
}

func TestInstallSummaryFallsBackToLocalhostInsteadOfPrivateSystemHostname(t *testing.T) {
	output := runInstallSummary(t, `
curl() { return 22; }
hostname() { printf '10.65.85.254\n'; }
ADMIN_HOSTNAME=
`)

	if !strings.Contains(output, "URL:       https://localhost:8443") {
		t.Fatalf("summary output did not reject private system hostname:\n%s", output)
	}
}

func TestInstallSummaryDoesNotUseIPv6LiteralAsHostnameFallback(t *testing.T) {
	output := runInstallSummary(t, `
curl() { return 22; }
hostname() { printf 'fe80::1\n'; }
ADMIN_HOSTNAME=::1
`)

	if !strings.Contains(output, "URL:       https://localhost:8443") {
		t.Fatalf("summary output did not reject IPv6 literal fallbacks:\n%s", output)
	}
}

func runSwapGuard(t *testing.T, commandMocks string) (string, string) {
	t.Helper()
	source, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	definitions := strings.TrimSuffix(strings.TrimSpace(string(source)), `main "$@"`)
	script := definitions + `
SWAP_CALLS="$(pwd)/swap-calls"
BUILD_SWAPFILE="$(pwd)/build-swap"
` + commandMocks + `
ensure_build_swap
cleanup_build_swap
`
	dir := t.TempDir()
	cmd := exec.Command("bash")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("swap guard script failed: %v\n%s", err, output)
	}
	ansiEscape := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiEscape.ReplaceAllString(string(output), ""), dir
}

func TestInstallerAddsTemporarySwapOnLowMemory(t *testing.T) {
	output, dir := runSwapGuard(t, `
awk() {
  case "$*" in
    *MemTotal*) printf '1024\n' ;;
    *SwapTotal*) printf '0\n' ;;
    *NR==2*) printf '14336\n' ;;
    *) return 0 ;;
  esac
}
df() { printf 'Filesystem 1M-blocks Used Available Use%% Mounted on\n/dev/vda1 20480 4096 14336 23%% /\n'; }
fallocate() { echo fallocate >> "$SWAP_CALLS"; }
mkswap() { echo mkswap >> "$SWAP_CALLS"; }
swapon() { echo swapon >> "$SWAP_CALLS"; }
swapoff() { echo swapoff >> "$SWAP_CALLS"; }
`)

	if !strings.Contains(output, "adding 2048MB temporary swap") || !strings.Contains(output, "Swap: 2048MB active") {
		t.Fatalf("installer did not add 2GB swap for a 1GB VPS:\n%s", output)
	}
	if !strings.Contains(output, "Build swap: removed") {
		t.Fatalf("installer did not remove the build swap afterwards:\n%s", output)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "swap-calls"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"fallocate", "mkswap", "swapon", "swapoff"} {
		if !strings.Contains(string(calls), expected) {
			t.Fatalf("swap setup did not run %s:\n%s", expected, string(calls))
		}
	}
}

func TestInstallerSkipsSwapWhenMemoryIsPlentiful(t *testing.T) {
	output, _ := runSwapGuard(t, `
awk() {
  case "$*" in
    *MemTotal*) printf '4096\n' ;;
    *SwapTotal*) printf '0\n' ;;
    *) return 0 ;;
  esac
}
swapon() { echo swapon >> "$SWAP_CALLS"; }
`)

	if strings.TrimSpace(output) != "" {
		t.Fatalf("installer added a swap despite ample memory:\n%s", output)
	}
}

func TestInstallerWarnsWhenSwapCannotBeEnabled(t *testing.T) {
	output, _ := runSwapGuard(t, `
awk() {
  case "$*" in
    *MemTotal*) printf '1024\n' ;;
    *SwapTotal*) printf '0\n' ;;
    *NR==2*) printf '14336\n' ;;
    *) return 0 ;;
  esac
}
df() { printf 'Filesystem 1M-blocks Used Available Use%% Mounted on\n/dev/vda1 20480 4096 14336 23%% /\n'; }
fallocate() { :; }
mkswap() { :; }
swapon() { return 1; }
swapoff() { echo swapoff >> "$SWAP_CALLS"; }
`)

	if !strings.Contains(output, "Could not enable swap") {
		t.Fatalf("installer did not warn about failed swapon:\n%s", output)
	}
	if strings.Contains(output, "Build swap: removed") || strings.Contains(output, "swapoff") {
		t.Fatalf("swapoff ran although swap was never enabled:\n%s", output)
	}
}

func TestInstallerWiresSwapGuardIntoBuildStep(t *testing.T) {
	source, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(source)
	ensureIndex := strings.Index(script, "    ensure_build_swap\n")
	cleanupIndex := strings.Index(script, "    cleanup_build_swap\n")
	cloneIndex := strings.Index(script, "git clone")
	verifyIndex := strings.Index(script, "Binary verification failed")
	if ensureIndex < 0 || ensureIndex > cloneIndex {
		t.Fatalf("installer does not enable the build swap before cloning the source:\n%s", script)
	}
	if cleanupIndex < verifyIndex {
		t.Fatalf("installer removes the build swap before the binary is verified:\n%s", script)
	}
}
