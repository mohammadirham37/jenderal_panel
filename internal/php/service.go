package php

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// supportedVersions lists the PHP versions that the panel can manage.
var supportedVersions = []string{"8.1", "8.2", "8.3", "8.4"}

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

	// Add ondrej/php PPA if not already present (required for multiple PHP versions)
	s.exec.RunSudo(ctx, "add-apt-repository", "-y", "ppa:ondrej/php")
	s.exec.RunSudo(ctx, "apt-get", "update")

	packages := []string{
		"install", "-y",
		"php" + version + "-fpm",
		"php" + version + "-cli",
		"php" + version + "-common",
		"php" + version + "-mysql",
		"php" + version + "-pgsql",
		"php" + version + "-mbstring",
		"php" + version + "-xml",
		"php" + version + "-curl",
		"php" + version + "-zip",
		"php" + version + "-gd",
		"php" + version + "-intl",
		"php" + version + "-bcmath",
	}

	result, err := s.exec.RunSudo(ctx, "apt-get", packages...)
	if err != nil {
		return fmt.Errorf("install php %s: %w", version, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("install php %s: %s", version, strings.TrimSpace(result.Stderr))
	}
	return nil
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
