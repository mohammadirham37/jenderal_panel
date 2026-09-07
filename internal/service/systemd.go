package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// ServiceManager defines the interface for managing system services.
type ServiceManager interface {
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Reload(ctx context.Context, name string) error
	Status(ctx context.Context, name string) (*model.ServiceStatus, error)
	List(ctx context.Context, name string) ([]model.ServiceStatus, error)
}

// Systemd implements ServiceManager using systemctl commands.
type Systemd struct {
	exec    executor.CommandExecutor
	allowed []string
}

// NewSystemd creates a new Systemd service manager with the given executor
// and allowed service name patterns (supports filepath.Match glob syntax).
func NewSystemd(exec executor.CommandExecutor, allowed []string) *Systemd {
	return &Systemd{exec: exec, allowed: allowed}
}

// isAllowed checks whether a service name matches any pattern in the allowed list.
func (s *Systemd) isAllowed(name string) bool {
	for _, pattern := range s.allowed {
		if matched, err := filepath.Match(pattern, name); err == nil && matched {
			return true
		}
	}
	return false
}

// checkAllowed returns ErrServiceNotAllowed if the service is not in the allowed list.
func (s *Systemd) checkAllowed(name string) error {
	if !s.isAllowed(name) {
		return model.ErrServiceNotAllowed
	}
	return nil
}

// Start starts the named service.
func (s *Systemd) Start(ctx context.Context, name string) error {
	if err := s.checkAllowed(name); err != nil {
		return err
	}
	result, err := s.exec.RunSudo(ctx, "systemctl", "start", name)
	if err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("start %s: %s", name, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Stop stops the named service.
func (s *Systemd) Stop(ctx context.Context, name string) error {
	if err := s.checkAllowed(name); err != nil {
		return err
	}
	result, err := s.exec.RunSudo(ctx, "systemctl", "stop", name)
	if err != nil {
		return fmt.Errorf("stop %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("stop %s: %s", name, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Restart restarts the named service.
func (s *Systemd) Restart(ctx context.Context, name string) error {
	if err := s.checkAllowed(name); err != nil {
		return err
	}
	result, err := s.exec.RunSudo(ctx, "systemctl", "restart", name)
	if err != nil {
		return fmt.Errorf("restart %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("restart %s: %s", name, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Reload reloads the named service.
func (s *Systemd) Reload(ctx context.Context, name string) error {
	if err := s.checkAllowed(name); err != nil {
		return err
	}
	result, err := s.exec.RunSudo(ctx, "systemctl", "reload", name)
	if err != nil {
		return fmt.Errorf("reload %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("reload %s: %s", name, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Status returns the status of the named service by parsing the output of
// systemctl show --property=ActiveState,SubState,MainPID,UnitFileState.
func (s *Systemd) Status(ctx context.Context, name string) (*model.ServiceStatus, error) {
	if err := s.checkAllowed(name); err != nil {
		return nil, err
	}
	result, err := s.exec.RunSudo(ctx, "systemctl", "show",
		"--property=ActiveState,SubState,MainPID,UnitFileState", name)
	if err != nil {
		return nil, fmt.Errorf("status %s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("status %s: %s", name, strings.TrimSpace(result.Stderr))
	}

	return parseStatus(name, result.Stdout)
}

// parseStatus parses systemctl show output into a ServiceStatus.
func parseStatus(name, output string) (*model.ServiceStatus, error) {
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

	status := &model.ServiceStatus{
		Name:    name,
		Active:  props["ActiveState"] == "active",
		Running: props["SubState"] == "running",
		Enabled: props["UnitFileState"] == "enabled",
	}

	if pidStr, ok := props["MainPID"]; ok {
		pid, err := strconv.Atoi(pidStr)
		if err == nil {
			status.PID = pid
		}
	}

	return status, nil
}

// List returns a list with the status of the named service.
func (s *Systemd) List(ctx context.Context, name string) ([]model.ServiceStatus, error) {
	status, err := s.Status(ctx, name)
	if err != nil {
		return nil, err
	}
	return []model.ServiceStatus{*status}, nil
}
