// Package dbconfig reads and applies the server-wide MySQL and PostgreSQL
// configuration files: the raw file for manual editing, plus validation and
// service restart with automatic rollback when a broken config is applied.
// The form mode lives in the frontend, which edits the same raw content.
package dbconfig

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

const (
	EngineMySQL      = "mysql"
	EnginePostgreSQL = "postgresql"
)

// mysqlConfigCandidates are the main server config files in the order they
// should be preferred; the first existing one wins.
var mysqlConfigCandidates = []string{
	"/etc/mysql/mysql.conf.d/mysqld.cnf",      // MySQL (Ubuntu apt layout)
	"/etc/mysql/mariadb.conf.d/50-server.cnf", // MariaDB (Ubuntu apt layout)
	"/etc/my.cnf",                             // RHEL-style layouts
}

// Config is the API view of one engine's server configuration.
type Config struct {
	Engine    string `json:"engine"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Available bool   `json:"available"`
}

// Service manages the database server configuration files.
type Service struct {
	exec  executor.CommandExecutor
	audit *audit.Service
}

// NewService creates a new dbconfig Service.
func NewService(exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	return &Service{exec: exec, audit: auditSvc}
}

// Get returns the engine's config file content and location. An engine whose
// config cannot be found comes back with Available=false instead of an error,
// so the page can say "not installed" rather than fail.
func (s *Service) Get(ctx context.Context, engine string) (*Config, error) {
	if engine != EngineMySQL && engine != EnginePostgreSQL {
		return nil, model.NewValidationError("unsupported engine: " + engine)
	}
	path, err := s.configPath(ctx, engine)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return &Config{Engine: engine, Available: false}, nil
	}
	result, err := s.exec.RunSudo(ctx, "cat", path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("read %s: %s", path, strings.TrimSpace(result.Stderr))
	}
	return &Config{Engine: engine, Path: path, Content: result.Stdout, Available: true}, nil
}

// Apply writes the config, validates it where the engine supports a
// validation mode, restarts the service, and rolls everything back when the
// service fails to come back up.
func (s *Service) Apply(ctx context.Context, engine, content string) error {
	if engine != EngineMySQL && engine != EnginePostgreSQL {
		return model.NewValidationError("unsupported engine: " + engine)
	}
	if strings.TrimSpace(content) == "" {
		return model.NewValidationError("config content is empty")
	}
	path, err := s.configPath(ctx, engine)
	if err != nil {
		return err
	}
	if path == "" {
		return model.NewDomainError("DB_CONFIG_NOT_FOUND", "no configuration file found for "+engine, nil)
	}

	tmpPath := fmt.Sprintf("/tmp/jenderal_dbconfig_%s.conf", engine)
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	defer os.Remove(tmpPath)

	backup := path + ".jenderal-bak"
	if err := s.runOK(ctx, "cp", path, backup); err != nil {
		return fmt.Errorf("backup config: %w", err)
	}
	if err := s.runOK(ctx, "cp", tmpPath, path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	if output, ok := s.validateMySQLConfig(ctx, engine); !ok {
		s.restore(ctx, engine, path, backup)
		return model.NewDomainError("DB_CONFIG_INVALID", output, nil)
	}

	if err := s.runOK(ctx, "systemctl", "restart", serviceName(engine)); err != nil {
		s.restore(ctx, engine, path, backup)
		return model.NewDomainError("DB_CONFIG_APPLY_FAILED",
			"service restart failed, previous configuration restored: "+err.Error(), nil)
	}
	check, err := s.exec.RunSudo(ctx, "systemctl", "is-active", serviceName(engine))
	if err != nil || check.ExitCode != 0 {
		detail := ""
		if err == nil {
			detail = strings.TrimSpace(check.Stdout + " " + check.Stderr)
		}
		s.restore(ctx, engine, path, backup)
		return model.NewDomainError("DB_CONFIG_APPLY_FAILED",
			"service did not come back up, previous configuration restored: "+strings.TrimSpace(detail), nil)
	}
	return nil
}

// configPath resolves the engine's main config file: empty when no config
// exists (engine not installed). PostgreSQL's path contains the installed
// major version, so it is discovered per server.
func (s *Service) configPath(ctx context.Context, engine string) (string, error) {
	switch engine {
	case EngineMySQL:
		for _, candidate := range mysqlConfigCandidates {
			exists, err := s.fileExists(ctx, candidate)
			if err != nil {
				return "", err
			}
			if exists {
				return candidate, nil
			}
		}
		return "", nil
	case EnginePostgreSQL:
		result, err := s.exec.RunSudo(ctx, "bash", "-c", "ls /etc/postgresql 2>/dev/null")
		if err != nil {
			return "", fmt.Errorf("list postgresql versions: %w", err)
		}
		if result.ExitCode != 0 {
			return "", nil
		}
		var versions []string
		for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
			if v := strings.TrimSpace(line); v != "" {
				versions = append(versions, v)
			}
		}
		// Newest major version first.
		sort.Sort(sort.Reverse(sort.StringSlice(versions)))
		for _, v := range versions {
			candidate := "/etc/postgresql/" + v + "/main/postgresql.conf"
			exists, err := s.fileExists(ctx, candidate)
			if err != nil {
				return "", err
			}
			if exists {
				return candidate, nil
			}
		}
		return "", nil
	default:
		return "", model.NewValidationError("unsupported engine: " + engine)
	}
}

func (s *Service) fileExists(ctx context.Context, path string) (bool, error) {
	result, err := s.exec.RunSudo(ctx, "test", "-f", path)
	if err != nil {
		return false, fmt.Errorf("check %s: %w", path, err)
	}
	return result.ExitCode == 0, nil
}

// validateMySQLConfig runs mysqld --validate-config when the engine supports
// it. MariaDB has no such flag — its "unknown option" output names the flag
// itself, which is treated as "no validation available", not as a broken
// config. A genuinely invalid config reports its error output.
func (s *Service) validateMySQLConfig(ctx context.Context, engine string) (string, bool) {
	if engine != EngineMySQL {
		return "", true
	}
	result, err := s.exec.RunSudo(ctx, "mysqld", "--validate-config")
	if err != nil {
		// Validator binary unavailable: restart + rollback still guards.
		return "", true
	}
	if result.ExitCode == 0 {
		return "", true
	}
	output := strings.TrimSpace(result.Stderr)
	if output == "" {
		output = strings.TrimSpace(result.Stdout)
	}
	if strings.Contains(output, "validate-config") {
		return "", true
	}
	if output == "" {
		output = "mysqld --validate-config failed"
	}
	return output, false
}

// restore puts the backup back and restarts so the engine keeps running the
// last known-good configuration.
func (s *Service) restore(ctx context.Context, engine, path, backup string) {
	_, _ = s.exec.RunSudo(ctx, "cp", backup, path)
	_, _ = s.exec.RunSudo(ctx, "systemctl", "restart", serviceName(engine))
}

func (s *Service) runOK(ctx context.Context, name string, args ...string) error {
	result, err := s.exec.RunSudo(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("%s: %s", name, strings.TrimSpace(result.Stderr))
	}
	return nil
}

func serviceName(engine string) string {
	if engine == EngineMySQL {
		return "mysql"
	}
	return "postgresql"
}
