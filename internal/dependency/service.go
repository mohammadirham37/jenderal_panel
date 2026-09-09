package dependency

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
)

// Status describes one developer dependency managed by the panel.
type Status struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

// Service inspects and manages developer command-line dependencies.
type Service struct {
	exec executor.CommandExecutor
}

func NewService(exec executor.CommandExecutor) *Service {
	return &Service{exec: exec}
}

func (s *Service) ComposerStatus(ctx context.Context) (Status, error) {
	status := Status{Name: "composer"}
	result, err := s.exec.Run(ctx, "/usr/local/bin/composer", "--version", "--no-ansi")
	if errors.Is(err, exec.ErrNotFound) {
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("check Composer: %w", err)
	}
	if result.ExitCode != 0 {
		return status, nil
	}

	fields := strings.Fields(strings.TrimSpace(result.Stdout))
	if len(fields) < 3 || fields[0] != "Composer" || fields[1] != "version" {
		return status, fmt.Errorf("parse Composer version: unexpected output %q", strings.TrimSpace(result.Stdout))
	}
	status.Installed = true
	status.Version = fields[2]
	return status, nil
}

const composerInstallScript = `set -euo pipefail
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

curl --fail --silent --show-error --location \
    --connect-timeout 10 --max-time 60 \
    https://composer.github.io/installer.sig \
    -o "$work_dir/expected"
curl --fail --silent --show-error --location \
    --connect-timeout 10 --max-time 60 \
    https://getcomposer.org/installer \
    -o "$work_dir/composer-setup.php"

expected="$(tr -d '\r\n' < "$work_dir/expected")"
actual="$(php -r "echo hash_file('sha384', '$work_dir/composer-setup.php');")"
test "$actual" = "$expected"

php "$work_dir/composer-setup.php" --2 --install-dir="$work_dir" --filename=composer
install -o root -g root -m 0755 "$work_dir/composer" /usr/local/bin/composer.new
mv -f /usr/local/bin/composer.new /usr/local/bin/composer
/usr/local/bin/composer --version --no-ansi
`

func (s *Service) ComposerInstallCommands() [][]string {
	return [][]string{{"bash", "-c", composerInstallScript}}
}

func (s *Service) ComposerUpdateCommands() [][]string {
	return [][]string{
		{"/usr/local/bin/composer", "self-update", "--2", "--no-interaction", "--no-ansi"},
		{"/usr/local/bin/composer", "--version", "--no-ansi"},
	}
}
