// Package sshserver reads and changes the OpenSSH listen port safely. Port
// changes are two-phase: the first phase keeps the old port accepting
// connections until the operator confirms the new one works, and only the
// finalize step drops it.
package sshserver

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// dropInPath is the panel-managed sshd configuration drop-in. Ubuntu's default
// sshd_config includes /etc/ssh/sshd_config.d/*.conf from the top of the file.
const dropInPath = "/etc/ssh/sshd_config.d/99-jenderal.conf"

const (
	settingOldPort = "ssh.port_change.old"
	settingNewPort = "ssh.port_change.new"
	sshUnit        = "ssh.service"
)

var portLineRegex = regexp.MustCompile(`(?m)^port\s+(\d+)\s*$`)

// Service manages the sshd listen port.
type Service struct {
	db        *sql.DB
	exec      executor.CommandExecutor
	audit     *audit.Service
	panelPort int
}

// NewService creates a new sshserver Service. panelPort is the port the panel
// itself listens on; changing SSH to it would lock operators out of the panel.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service, panelPort int) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc, panelPort: panelPort}
}

// Status is the API view of the SSH port state.
type Status struct {
	CurrentPort int        `json:"current_port"`
	PanelPort   int        `json:"panel_port"`
	Pending     *PortShift `json:"pending_change"`
}

// PortShift describes an in-flight two-phase port change.
type PortShift struct {
	OldPort int `json:"old_port"`
	NewPort int `json:"new_port"`
}

// CurrentPort returns the effective sshd port parsed from `sshd -T`, which
// reflects all included configuration without a restart.
func (s *Service) CurrentPort(ctx context.Context) (int, error) {
	result, err := s.exec.RunSudo(ctx, "sshd", "-T")
	if err != nil {
		return 0, fmt.Errorf("read sshd config: %w", err)
	}
	if result.ExitCode != 0 {
		return 0, fmt.Errorf("read sshd config: %s", strings.TrimSpace(result.Stderr))
	}
	m := portLineRegex.FindStringSubmatch(result.Stdout)
	if m == nil {
		// sshd -T always reports a port; default to 22 when absent.
		return 22, nil
	}
	port, err := strconv.Atoi(m[1])
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("sshd reported invalid port %q", m[1])
	}
	return port, nil
}

// Status returns the current port plus any pending change.
func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{PanelPort: s.panelPort}
	current, err := s.CurrentPort(ctx)
	if err != nil {
		return status, err
	}
	status.CurrentPort = current
	if old, new, ok, err := s.pendingChange(ctx); err != nil {
		return status, err
	} else if ok {
		status.Pending = &PortShift{OldPort: old, NewPort: new}
	}
	return status, nil
}

// BeginChange starts a two-phase port change: write a drop-in accepting both
// ports, validate sshd config, open the new port in ufw, and restart sshd.
// The old port keeps working until Finalize.
func (s *Service) BeginChange(ctx context.Context, newPort int) error {
	if _, _, pending, err := s.pendingChange(ctx); err != nil {
		return err
	} else if pending {
		return model.NewValidationError("a port change is already in progress; finalize or cancel it first")
	}
	current, err := s.CurrentPort(ctx)
	if err != nil {
		return err
	}
	if err := s.validatePort(ctx, newPort, current); err != nil {
		return err
	}

	if err := s.writeDropIn(ctx, fmt.Sprintf("Port %d\nPort %d\n", current, newPort)); err != nil {
		return err
	}
	if err := s.testConfig(ctx, fmt.Sprintf("Port %d\nPort %d\n", current, newPort)); err != nil {
		return err
	}
	if err := s.ufwAllow(ctx, newPort); err != nil {
		return err
	}
	if err := s.restartSSH(ctx); err != nil {
		return err
	}

	if err := s.savePendingChange(ctx, current, newPort); err != nil {
		return err
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_port_change_start", Module: "ssh", Detail: fmt.Sprintf("changing SSH port from %d to %d", current, newPort)})
	return nil
}

// Finalize completes a pending change: only the new port remains in the
// drop-in and the old ufw rule is removed.
func (s *Service) Finalize(ctx context.Context) error {
	oldPort, newPort, pending, err := s.pendingChange(ctx)
	if err != nil {
		return err
	}
	if !pending {
		return model.NewValidationError("no port change is in progress")
	}

	content := fmt.Sprintf("Port %d\n", newPort)
	if err := s.writeDropIn(ctx, content); err != nil {
		return err
	}
	if err := s.testConfig(ctx, content); err != nil {
		return err
	}
	if err := s.restartSSH(ctx); err != nil {
		return err
	}
	// Best effort: an already-deleted rule is not an error.
	_, _ = s.exec.RunSudo(ctx, "ufw", "delete", "allow", fmt.Sprintf("%d/tcp", oldPort))

	if err := s.clearPendingChange(ctx); err != nil {
		return err
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_port_change_finalize", Module: "ssh", Detail: fmt.Sprintf("finalized SSH port change from %d to %d", oldPort, newPort)})
	return nil
}

// Cancel rolls a pending change back to the old port and removes the new ufw
// rule.
func (s *Service) Cancel(ctx context.Context) error {
	oldPort, newPort, pending, err := s.pendingChange(ctx)
	if err != nil {
		return err
	}
	if !pending {
		return model.NewValidationError("no port change is in progress")
	}

	content := fmt.Sprintf("Port %d\n", oldPort)
	if err := s.writeDropIn(ctx, content); err != nil {
		return err
	}
	if err := s.testConfig(ctx, content); err != nil {
		return err
	}
	if err := s.restartSSH(ctx); err != nil {
		return err
	}
	_, _ = s.exec.RunSudo(ctx, "ufw", "delete", "allow", fmt.Sprintf("%d/tcp", newPort))

	if err := s.clearPendingChange(ctx); err != nil {
		return err
	}
	_ = s.audit.Log(ctx, audit.LogEntry{Action: "ssh_port_change_cancel", Module: "ssh", Detail: fmt.Sprintf("cancelled SSH port change from %d to %d", oldPort, newPort)})
	return nil
}

// validatePort rejects unsafe port choices: out of range, equal to the current
// port, the panel's own port, or ports claimed by managed applications.
func (s *Service) validatePort(ctx context.Context, newPort, currentPort int) error {
	if newPort < 1 || newPort > 65535 {
		return model.NewValidationError("port must be between 1 and 65535")
	}
	if newPort == currentPort {
		return model.NewValidationError(fmt.Sprintf("port %d is already the active SSH port", newPort))
	}
	if newPort == s.panelPort {
		return model.NewValidationError(fmt.Sprintf("port %d is used by the panel itself", newPort))
	}
	used, err := s.managedPorts(ctx)
	if err != nil {
		return err
	}
	if used[newPort] {
		return model.NewValidationError(fmt.Sprintf("port %d is already used by a managed website or application", newPort))
	}
	return nil
}

// managedPorts collects ports the panel has handed out: Octane workers and
// Node.js applications.
func (s *Service) managedPorts(ctx context.Context) (map[int]bool, error) {
	used := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT octane_port FROM websites WHERE octane_port > 0`)
	if err != nil {
		return nil, fmt.Errorf("list website ports: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			return nil, fmt.Errorf("scan website ports: %w", err)
		}
		used[port] = true
	}
	rows2, err := s.db.QueryContext(ctx, `SELECT port FROM nodejs_apps WHERE port > 0`)
	if err != nil {
		return nil, fmt.Errorf("list app ports: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var port int
		if err := rows2.Scan(&port); err != nil {
			return nil, fmt.Errorf("scan app ports: %w", err)
		}
		used[port] = true
	}
	return used, nil
}

func (s *Service) pendingChange(ctx context.Context) (oldPort, newPort int, pending bool, err error) {
	oldStr, err := s.readSetting(ctx, settingOldPort)
	if err != nil {
		return 0, 0, false, err
	}
	newStr, err := s.readSetting(ctx, settingNewPort)
	if err != nil {
		return 0, 0, false, err
	}
	if oldStr == "" || newStr == "" {
		return 0, 0, false, nil
	}
	oldPort, err = strconv.Atoi(oldStr)
	if err != nil {
		return 0, 0, false, fmt.Errorf("corrupt pending port change: %w", err)
	}
	newPort, err = strconv.Atoi(newStr)
	if err != nil {
		return 0, 0, false, fmt.Errorf("corrupt pending port change: %w", err)
	}
	return oldPort, newPort, true, nil
}

func (s *Service) savePendingChange(ctx context.Context, oldPort, newPort int) error {
	if err := s.writeSetting(ctx, settingOldPort, strconv.Itoa(oldPort)); err != nil {
		return err
	}
	return s.writeSetting(ctx, settingNewPort, strconv.Itoa(newPort))
}

func (s *Service) clearPendingChange(ctx context.Context) error {
	if err := s.deleteSetting(ctx, settingOldPort); err != nil {
		return err
	}
	return s.deleteSetting(ctx, settingNewPort)
}

func (s *Service) readSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read setting %s: %w", key, err)
	}
	return value, nil
}

func (s *Service) writeSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("write setting %s: %w", key, err)
	}
	return nil
}

func (s *Service) deleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("delete setting %s: %w", key, err)
	}
	return nil
}

// writeDropIn installs the drop-in atomically via a .new file.
func (s *Service) writeDropIn(ctx context.Context, content string) error {
	if _, err := s.exec.RunSudo(ctx, "mkdir", "-p", "/etc/ssh/sshd_config.d"); err != nil {
		return fmt.Errorf("prepare sshd_config.d: %w", err)
	}
	if _, err := s.exec.RunSudoWithInput(ctx, content, "tee", dropInPath+".new"); err != nil {
		return fmt.Errorf("write sshd drop-in: %w", err)
	}
	result, err := s.exec.RunSudo(ctx, "mv", "-f", dropInPath+".new", dropInPath)
	if err != nil {
		return fmt.Errorf("install sshd drop-in: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("install sshd drop-in: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

// testConfig runs sshd -t and restores previous content on failure so a bad
// port never leaves the server unable to boot sshd.
func (s *Service) testConfig(ctx context.Context, rollback string) error {
	result, err := s.exec.RunSudo(ctx, "sshd", "-t")
	if err != nil {
		return fmt.Errorf("test sshd config: %w", err)
	}
	if result.ExitCode != 0 {
		_, _ = s.exec.RunSudoWithInput(ctx, rollback, "tee", dropInPath)
		return model.NewDomainError("SSHD_CONFIG_INVALID",
			"sshd rejected the configuration: "+strings.TrimSpace(result.Stderr), nil)
	}
	return nil
}

func (s *Service) ufwAllow(ctx context.Context, port int) error {
	result, err := s.exec.RunSudo(ctx, "ufw", "allow", fmt.Sprintf("%d/tcp", port))
	if err != nil {
		return fmt.Errorf("open firewall port: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("open firewall port: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

func (s *Service) restartSSH(ctx context.Context) error {
	result, err := s.exec.RunSudo(ctx, "systemctl", "restart", sshUnit)
	if err != nil {
		return fmt.Errorf("restart sshd: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("restart sshd: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}
