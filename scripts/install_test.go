package scripts_test

import (
	"os"
	"os/exec"
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
