package php

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// The server-wide "php" command is managed by Debian alternatives. Websites
// run their own version through internal/phpcli; this default is what
// plain "php" means everywhere else (root shells, scripts outside a site).

var versionPattern = regexp.MustCompile(`^[0-9]\.[0-9]$`)

// cliAlternatives are the alternative groups switched together.
var cliAlternatives = []string{"php", "phar", "phar.phar"}

// CLIDefault returns the PHP version /usr/bin/php currently points at, or
// "" when PHP is not installed.
func (s *Service) CLIDefault(ctx context.Context) (string, error) {
	res, err := s.exec.Run(ctx, "readlink", "-f", "/usr/bin/php")
	if err != nil {
		return "", fmt.Errorf("resolve php: %w", err)
	}
	if res == nil || res.ExitCode != 0 {
		return "", nil
	}
	target := strings.TrimSpace(res.Stdout)
	version, ok := strings.CutPrefix(target, "/usr/bin/php")
	if !ok || !versionPattern.MatchString(version) {
		return "", nil
	}
	return version, nil
}

// SetCLIDefault points the server-wide php (and phar) commands at version.
func (s *Service) SetCLIDefault(ctx context.Context, version string) error {
	if err := s.validateVersion(ctx, version); err != nil {
		return err
	}
	binary := "/usr/bin/php" + version
	if res, err := s.exec.RunSudo(ctx, "test", "-x", binary); err != nil || res == nil || res.ExitCode != 0 {
		return model.NewValidationError("PHP " + version + " CLI is not installed")
	}
	for _, name := range cliAlternatives {
		target := "/usr/bin/" + name + version
		if name == "php" {
			target = binary
		}
		if res, err := s.exec.RunSudo(ctx, "test", "-e", target); err != nil || res == nil || res.ExitCode != 0 {
			continue // phar links are optional
		}
		res, err := s.exec.RunSudo(ctx, "update-alternatives", "--set", name, target)
		if err != nil {
			return fmt.Errorf("set %s alternative: %w", name, err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("set %s alternative: %s", name, strings.TrimSpace(res.Stderr))
		}
	}
	return nil
}
