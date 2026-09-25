// Package supervisor manages arbitrary long-running processes as systemd
// units (jenderal-proc-<id>.service). Each process gets its own root-owned
// env file so secrets never appear in the unit or in `ps` output.
package supervisor

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const (
	envDir     = "/etc/jenderal/proc"
	unitPrefix = "jenderal-proc-"
)

var (
	nameRegex    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	userRegex    = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	dirRegex     = regexp.MustCompile(`^/[\w./-]*$`)
	envLineRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=.*$`)
)

// Service manages supervised processes.
type Service struct {
	db    *sql.DB
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new supervisor Service.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{db: db, exec: exec, audit: auditSvc}
}

// unitName returns the systemd unit for a process ID.
func unitName(id string) string { return unitPrefix + id + ".service" }

// envPath returns the root-only env file for a process ID.
func envPath(id string) string { return envDir + "/" + id + ".env" }

// validateRequest checks user-supplied process fields. Everything ends up in
// a systemd unit, an env file, or shell context, so the rules are strict.
func validateRequest(req model.SupervisorProcessRequest) (bool, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Command = strings.TrimSpace(req.Command)
	req.WorkingDir = strings.TrimSpace(req.WorkingDir)
	req.RunAs = strings.TrimSpace(req.RunAs)
	if !nameRegex.MatchString(req.Name) {
		return false, model.NewValidationError("process name must be 1-64 chars: letters, digits, dot, dash, underscore")
	}
	if req.Command == "" {
		return false, model.NewValidationError("command is required")
	}
	if strings.ContainsAny(req.Command, "\n\r") {
		return false, model.NewValidationError("command must be a single line")
	}
	if req.WorkingDir != "" && !dirRegex.MatchString(req.WorkingDir) {
		return false, model.NewValidationError("working directory must be an absolute path")
	}
	if !userRegex.MatchString(req.RunAs) || req.RunAs == "root" {
		return false, model.NewValidationError("run-as must be an existing non-root system user")
	}
	for _, line := range strings.Split(req.Env, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !envLineRegex.MatchString(line) {
			return false, model.NewValidationError("env lines must look like KEY=value")
		}
	}
	return req.AutoRestart == nil || *req.AutoRestart, nil
}

// RenderProcessUnit renders the systemd unit for a process. The command runs
// under bash -lc so shell syntax (env vars, pipes) works; single quotes in
// the command are escaped for systemd's quote parser.
func RenderProcessUnit(id string, name, command, workingDir, runAs string, autoRestart bool) string {
	restart := "no"
	if autoRestart {
		restart = "always"
	}
	escaped := strings.ReplaceAll(command, `'`, `'\''`)
	unit := "[Unit]\nDescription=Jenderal Process " + name + "\nAfter=network.target\n\n[Service]\n"
	unit += "Type=simple\n"
	unit += "User=" + runAs + "\n"
	if workingDir != "" {
		unit += "WorkingDirectory=" + workingDir + "\n"
	}
	unit += "EnvironmentFile=" + envPath(id) + "\n"
	unit += "ExecStart=/bin/bash -lc '" + escaped + "'\n"
	unit += "Restart=" + restart + "\n"
	if autoRestart {
		unit += "RestartSec=5\n"
	}
	unit += "\n[Install]\nWantedBy=multi-user.target\n"
	return unit
}

// renderProcEnv normalizes env lines for the root-only env file.
func renderProcEnv(env string) string {
	var b strings.Builder
	for _, line := range strings.Split(env, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func scanProcess(row interface{ Scan(...any) error }) (model.SupervisorProcess, error) {
	var p model.SupervisorProcess
	var autoRestart int
	var createdStr, updatedStr string
	err := row.Scan(&p.ID, &p.Name, &p.Command, &p.WorkingDir, &p.RunAs, &p.Env, &autoRestart, &p.Status, &p.CreatedBy, &createdStr, &updatedStr)
	if err != nil {
		return p, err
	}
	p.AutoRestart = autoRestart == 1
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return p, nil
}

const processColumns = `id, name, command, working_dir, run_as, env, auto_restart, status, created_by, created_at, updated_at`

// List returns all supervised processes with their live systemd state.
func (s *Service) List(ctx context.Context) ([]model.SupervisorProcess, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+processColumns+` FROM supervisor_processes ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	defer rows.Close()

	var processes []model.SupervisorProcess
	for rows.Next() {
		p, err := scanProcess(rows)
		if err != nil {
			return nil, fmt.Errorf("scan process: %w", err)
		}
		p.Status = s.liveStatus(ctx, p.ID)
		processes = append(processes, p)
	}
	return processes, rows.Err()
}

// Get returns one supervised process.
func (s *Service) Get(ctx context.Context, id string) (model.SupervisorProcess, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+processColumns+` FROM supervisor_processes WHERE id = ?`, id)
	p, err := scanProcess(row)
	if err != nil {
		return model.SupervisorProcess{}, model.ErrNotFound
	}
	p.Status = s.liveStatus(ctx, p.ID)
	return p, nil
}

// liveStatus asks systemd for the unit's active state.
func (s *Service) liveStatus(ctx context.Context, id string) string {
	result, err := s.exec.RunSudo(ctx, "systemctl", "is-active", unitName(id))
	if err != nil || result == nil {
		return "unknown"
	}
	state := strings.TrimSpace(result.Stdout)
	switch state {
	case "active", "inactive", "failed", "activating":
		return state
	default:
		return "unknown"
	}
}

// Create validates, persists, installs the env file + unit, and starts the
// process.
func (s *Service) Create(ctx context.Context, req model.SupervisorProcessRequest, createdBy string) (model.SupervisorProcess, error) {
	autoRestart, err := validateRequest(req)
	if err != nil {
		return model.SupervisorProcess{}, err
	}

	id := ulid.Make().String()
	now := time.Now().UTC().Format(time.RFC3339)
	env := renderProcEnv(req.Env)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO supervisor_processes (id, name, command, working_dir, run_as, env, auto_restart, status, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'running', ?, ?, ?)`,
		id, strings.TrimSpace(req.Name), strings.TrimSpace(req.Command), strings.TrimSpace(req.WorkingDir),
		strings.TrimSpace(req.RunAs), env, boolToInt(autoRestart), createdBy, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.SupervisorProcess{}, model.NewValidationError("a process with this name already exists")
		}
		return model.SupervisorProcess{}, fmt.Errorf("insert process: %w", err)
	}

	if err := s.writeAssets(ctx, id, req, autoRestart); err != nil {
		return model.SupervisorProcess{}, err
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", "enable", "--now", unitName(id)); err != nil {
		return model.SupervisorProcess{}, fmt.Errorf("start process: %w", err)
	}
	return s.Get(ctx, id)
}

// writeAssets installs the env file and the unit, then reloads systemd.
func (s *Service) writeAssets(ctx context.Context, id string, req model.SupervisorProcessRequest, autoRestart bool) error {
	unit := RenderProcessUnit(id, strings.TrimSpace(req.Name), strings.TrimSpace(req.Command), strings.TrimSpace(req.WorkingDir), strings.TrimSpace(req.RunAs), autoRestart)

	if _, err := s.exec.RunSudo(ctx, "mkdir", "-p", envDir); err != nil {
		return fmt.Errorf("create env directory: %w", err)
	}
	if _, err := s.exec.RunSudoWithInput(ctx, renderProcEnv(req.Env), "tee", envPath(id)); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "chmod", "0600", envPath(id)); err != nil {
		return fmt.Errorf("mode env file: %w", err)
	}
	unitPath := "/etc/systemd/system/" + unitName(id)
	if _, err := s.exec.RunSudo(ctx, "bash", "-c",
		fmt.Sprintf("cat > %s << 'UNITEOF'\n%sUNITEOF", unitPath, unit)); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return nil
}

// Update rewrites the process definition and restarts it when it was running.
func (s *Service) Update(ctx context.Context, id string, req model.SupervisorProcessRequest) (model.SupervisorProcess, error) {
	autoRestart, err := validateRequest(req)
	if err != nil {
		return model.SupervisorProcess{}, err
	}
	existing, err := s.Get(ctx, id)
	if err != nil {
		return model.SupervisorProcess{}, err
	}

	wasActive := existing.Status == "active"
	env := renderProcEnv(req.Env)
	_, err = s.db.ExecContext(ctx,
		`UPDATE supervisor_processes SET name = ?, command = ?, working_dir = ?, run_as = ?, env = ?, auto_restart = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(req.Name), strings.TrimSpace(req.Command), strings.TrimSpace(req.WorkingDir),
		strings.TrimSpace(req.RunAs), env, boolToInt(autoRestart), time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return model.SupervisorProcess{}, model.NewValidationError("a process with this name already exists")
		}
		return model.SupervisorProcess{}, fmt.Errorf("update process: %w", err)
	}

	if err := s.writeAssets(ctx, id, req, autoRestart); err != nil {
		return model.SupervisorProcess{}, err
	}
	if wasActive {
		if _, err := s.exec.RunSudo(ctx, "systemctl", "restart", unitName(id)); err != nil {
			return model.SupervisorProcess{}, fmt.Errorf("restart process: %w", err)
		}
	}
	return s.Get(ctx, id)
}

// Delete stops the process and removes its unit, env file, and row.
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	_, _ = s.exec.RunSudo(ctx, "systemctl", "stop", unitName(id))
	_, _ = s.exec.RunSudo(ctx, "systemctl", "disable", unitName(id))
	_, _ = s.exec.RunSudo(ctx, "rm", "-f", "/etc/systemd/system/"+unitName(id), envPath(id))
	if _, err := s.exec.RunSudo(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM supervisor_processes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete process: %w", err)
	}
	return nil
}

// Action runs start/stop/restart for a process.
func (s *Service) Action(ctx context.Context, id, action string) error {
	switch action {
	case "start", "stop", "restart":
	default:
		return model.NewValidationError("action must be start, stop or restart")
	}
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if _, err := s.exec.RunSudo(ctx, "systemctl", action, unitName(id)); err != nil {
		return fmt.Errorf("%s process: %w", action, err)
	}
	return nil
}

// Logs tails the process's journald log.
func (s *Service) Logs(ctx context.Context, id string, lines int) (string, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return "", err
	}
	if lines <= 0 {
		lines = 100
	}
	if lines > 500 {
		lines = 500
	}
	result, err := s.exec.RunSudo(ctx, "journalctl", "-u", unitName(id), "-n", strconv.Itoa(lines), "--no-pager")
	if err != nil {
		return "", fmt.Errorf("read process logs: %w", err)
	}
	if result == nil {
		return "", nil
	}
	return result.Stdout, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
