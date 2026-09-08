package php

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// supportedVersions lists the PHP versions that the panel can manage.
var supportedVersions = []string{"8.1", "8.2", "8.3", "8.4"}

const phpRepositorySetupScript = `set -euo pipefail
keyring=/usr/share/keyrings/ondrej-php.gpg
source_list=/etc/apt/sources.list.d/ondrej-php.list
fingerprint=B8DC7E53946656EFBCE4C1DD71DAEAAB4AD4CAB6
key_url='https://keyserver.ubuntu.com/pks/lookup?op=get&search=0xB8DC7E53946656EFBCE4C1DD71DAEAAB4AD4CAB6'

. /etc/os-release
case "${VERSION_CODENAME:-}" in
    jammy|noble) ;;
    *) echo "Unsupported Ubuntu codename: ${VERSION_CODENAME:-unknown}" >&2; exit 1 ;;
esac

key_fingerprint() {
    gpg --batch --show-keys --with-colons "$1" 2>/dev/null | awk -F: '$1 == "fpr" { print $10; exit }'
}

primary_key_count() {
    gpg --batch --show-keys --with-colons "$1" 2>/dev/null | awk -F: '$1 == "pub" { count++ } END { print count + 0 }'
}

key_is_valid() {
    [ "$(primary_key_count "$1")" = "1" ] && [ "$(key_fingerprint "$1")" = "$fingerprint" ]
}

if [ ! -f "$keyring" ] || ! key_is_valid "$keyring"; then
    tmp_key="$(mktemp)"
    tmp_keyring="$(mktemp)"
    cleanup() { rm -f "$tmp_key" "$tmp_keyring"; }
    trap cleanup EXIT

    curl --fail --silent --show-error --location \
        --connect-timeout 10 --max-time 60 --retry 2 --retry-all-errors \
        "$key_url" --output "$tmp_key"

    if ! key_is_valid "$tmp_key"; then
        echo "Unexpected Ondrej PHP signing key: fingerprint=$(key_fingerprint "$tmp_key"), primary_keys=$(primary_key_count "$tmp_key")" >&2
        exit 1
    fi

    gpg --batch --yes --dearmor --output "$tmp_keyring" "$tmp_key"
    install -o root -g root -m 0644 "$tmp_keyring" "$keyring"
fi

rm -f /etc/apt/sources.list.d/ondrej-ubuntu-php-*.list \
    /etc/apt/sources.list.d/ondrej-ubuntu-php-*.sources
printf 'deb [signed-by=/usr/share/keyrings/ondrej-php.gpg] https://ppa.launchpadcontent.net/ondrej/php/ubuntu %s main\n' \
    "$VERSION_CODENAME" > "$source_list"
`

const phpInstallStepTimeout = 15 * time.Minute

// Service manages PHP-FPM installations and configuration.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new PHP management service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// ListInstalled returns the status of every supported PHP version.
func (s *Service) ListInstalled(ctx context.Context) ([]model.PHPVersion, error) {
	var versions []model.PHPVersion

	for _, ver := range supportedVersions {
		pv := model.PHPVersion{Version: ver}

		// Check whether /etc/php/{ver} exists.
		result, err := s.exec.Run(ctx, "test", "-d", "/etc/php/"+ver)
		if err != nil {
			return nil, fmt.Errorf("check php %s directory: %w", ver, err)
		}
		if result.ExitCode != 0 {
			versions = append(versions, pv)
			continue
		}
		pv.Installed = true

		// Query the FPM service status.
		svcName := "php" + ver + "-fpm"
		sResult, err := s.exec.Run(ctx, "systemctl", "show",
			"--property=ActiveState,UnitFileState", svcName)
		if err != nil {
			return nil, fmt.Errorf("systemctl show %s: %w", svcName, err)
		}
		if sResult.ExitCode == 0 {
			running, enabled := parseFPMStatus(sResult.Stdout)
			pv.Running = running
			pv.Enabled = enabled
		}

		versions = append(versions, pv)
	}

	return versions, nil
}

// IsInstalled reports whether the given PHP version directory exists.
func (s *Service) IsInstalled(ctx context.Context, version string) bool {
	result, err := s.exec.Run(ctx, "test", "-d", "/etc/php/"+version)
	if err != nil {
		return false
	}
	return result.ExitCode == 0
}

// Install installs the specified PHP version and common extensions.
func (s *Service) Install(ctx context.Context, version string) error {
	if err := s.validateVersion(version); err != nil {
		return err
	}

	for step, command := range s.installCommands(version) {
		stepCtx, cancel := context.WithTimeout(ctx, phpInstallStepTimeout)
		result, err := s.exec.RunSudo(stepCtx, command[0], command[1:]...)
		cancel()
		if err != nil {
			return fmt.Errorf("install php %s step %d: %w", version, step+1, err)
		}
		if result.ExitCode != 0 {
			detail := strings.TrimSpace(result.Stderr)
			if detail == "" {
				detail = strings.TrimSpace(result.Stdout)
			}
			return fmt.Errorf("install php %s step %d: %s", version, step+1, detail)
		}
	}
	return nil
}

func (s *Service) installCommands(version string) [][]string {
	aptTimeouts := []string{
		"-o", "Acquire::Retries=2",
		"-o", "Acquire::http::Timeout=30",
		"-o", "Acquire::https::Timeout=30",
	}
	updateArgs := append([]string{"update", "-qq"}, aptTimeouts...)
	installArgs := []string{
		"install", "-y", "-o", "DPkg::Lock::Timeout=120",
	}
	installArgs = append(installArgs, aptTimeouts...)
	installArgs = append(installArgs,
		"php"+version+"-fpm",
		"php"+version+"-cli",
		"php"+version+"-common",
		"php"+version+"-mysql",
		"php"+version+"-pgsql",
		"php"+version+"-mbstring",
		"php"+version+"-xml",
		"php"+version+"-curl",
		"php"+version+"-zip",
		"php"+version+"-gd",
		"php"+version+"-intl",
		"php"+version+"-bcmath",
	)
	dependencyArgs := []string{"install", "-y", "-o", "DPkg::Lock::Timeout=120"}
	dependencyArgs = append(dependencyArgs, aptTimeouts...)
	dependencyArgs = append(dependencyArgs, "ca-certificates", "curl", "gnupg")

	return [][]string{
		append([]string{"apt-get"}, updateArgs...),
		append([]string{"apt-get"}, dependencyArgs...),
		{"bash", "-c", phpRepositorySetupScript},
		append([]string{"apt-get"}, updateArgs...),
		append([]string{"apt-get"}, installArgs...),
	}
}

// Uninstall removes the specified PHP version packages.
func (s *Service) Uninstall(ctx context.Context, version string) error {
	if err := s.validateVersion(version); err != nil {
		return err
	}

	result, err := s.exec.RunSudo(ctx, "apt-get", "remove", "-y",
		"php"+version+"-fpm", "php"+version+"-*")
	if err != nil {
		return fmt.Errorf("uninstall php %s: %w", version, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("uninstall php %s: %s", version, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Start starts the PHP-FPM service for the given version.
func (s *Service) Start(ctx context.Context, version string) error {
	if err := s.validateVersion(version); err != nil {
		return err
	}
	return s.systemctlAction(ctx, "start", version)
}

// Stop stops the PHP-FPM service for the given version.
func (s *Service) Stop(ctx context.Context, version string) error {
	if err := s.validateVersion(version); err != nil {
		return err
	}
	return s.systemctlAction(ctx, "stop", version)
}

// Restart restarts the PHP-FPM service for the given version.
func (s *Service) Restart(ctx context.Context, version string) error {
	if err := s.validateVersion(version); err != nil {
		return err
	}
	return s.systemctlAction(ctx, "restart", version)
}

// GetPHPINI returns the contents of the FPM php.ini for the given version.
func (s *Service) GetPHPINI(ctx context.Context, version string) (string, error) {
	if err := s.validateVersion(version); err != nil {
		return "", err
	}

	path := fmt.Sprintf("/etc/php/%s/fpm/php.ini", version)
	result, err := s.exec.RunSudo(ctx, "cat", path)
	if err != nil {
		return "", fmt.Errorf("read php.ini for %s: %w", version, err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read php.ini for %s: %s", version, strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// SavePHPINI writes the given content to the FPM php.ini and restarts FPM.
func (s *Service) SavePHPINI(ctx context.Context, version, content string) error {
	if err := s.validateVersion(version); err != nil {
		return err
	}

	tmpPath := "/tmp/jenderal_php_ini.tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp php.ini: %w", err)
	}

	destPath := fmt.Sprintf("/etc/php/%s/fpm/php.ini", version)
	result, err := s.exec.RunSudo(ctx, "cp", tmpPath, destPath)
	if err != nil {
		return fmt.Errorf("write php.ini for %s: %w", version, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write php.ini for %s: %s", version, strings.TrimSpace(result.Stderr))
	}

	return s.systemctlAction(ctx, "restart", version)
}

// validateVersion checks that the version string is in the supported list.
func (s *Service) validateVersion(version string) error {
	for _, v := range supportedVersions {
		if v == version {
			return nil
		}
	}
	return model.NewValidationError("unsupported PHP version: " + version)
}

// systemctlAction runs a systemctl command for the given PHP-FPM version.
func (s *Service) systemctlAction(ctx context.Context, action, version string) error {
	svcName := "php" + version + "-fpm"
	result, err := s.exec.RunSudo(ctx, "systemctl", action, svcName)
	if err != nil {
		return fmt.Errorf("%s %s: %w", action, svcName, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("%s %s: %s", action, svcName, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// parseFPMStatus parses systemctl show output and returns running and enabled booleans.
func parseFPMStatus(output string) (running, enabled bool) {
	props := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			props[parts[0]] = parts[1]
		}
	}
	running = props["ActiveState"] == "active"
	enabled = props["UnitFileState"] == "enabled"
	return
}
