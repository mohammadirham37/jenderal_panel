package dbmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// pgSystemDBs are databases that should be filtered from listing results.
var pgSystemDBs = map[string]bool{
	"postgres":  true,
	"template0": true,
	"template1": true,
}

// pgSystemUsers are users that should be filtered from listing results.
var pgSystemUsers = map[string]bool{
	"postgres": true,
}

// PostgreSQLEngine implements DatabaseEngine for PostgreSQL.
type PostgreSQLEngine struct {
	exec executor.CommandExecutor
}

// NewPostgreSQLEngine creates a new PostgreSQL engine backed by the given executor.
func NewPostgreSQLEngine(exec executor.CommandExecutor) *PostgreSQLEngine {
	return &PostgreSQLEngine{exec: exec}
}

// Install installs PostgreSQL via apt-get.
func (p *PostgreSQLEngine) Install(ctx context.Context) error {
	result, err := p.exec.RunSudo(ctx, "apt-get", "install", "-y", "postgresql")
	if err != nil {
		return fmt.Errorf("postgresql install: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to install postgresql: "+result.Stderr, nil)
	}
	return nil
}

// Status returns the current status of PostgreSQL.
func (p *PostgreSQLEngine) Status(ctx context.Context) (model.EngineStatus, error) {
	status := model.EngineStatus{Name: "postgresql"}

	// Check if installed.
	verResult, err := p.exec.Run(ctx, "psql", "--version")
	if err != nil {
		return status, nil
	}
	if verResult.ExitCode != 0 {
		return status, nil
	}
	status.Installed = true
	status.Version = strings.TrimSpace(verResult.Stdout)

	// Check if running.
	sysResult, err := p.exec.RunSudo(ctx, "systemctl", "is-active", "postgresql")
	if err != nil {
		return status, nil
	}
	status.Running = strings.TrimSpace(sysResult.Stdout) == "active"

	return status, nil
}

// Start starts the PostgreSQL service.
func (p *PostgreSQLEngine) Start(ctx context.Context) error {
	result, err := p.exec.RunSudo(ctx, "systemctl", "start", "postgresql")
	if err != nil {
		return fmt.Errorf("postgresql start: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to start postgresql: "+result.Stderr, nil)
	}
	return nil
}

// Stop stops the PostgreSQL service.
func (p *PostgreSQLEngine) Stop(ctx context.Context) error {
	result, err := p.exec.RunSudo(ctx, "systemctl", "stop", "postgresql")
	if err != nil {
		return fmt.Errorf("postgresql stop: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to stop postgresql: "+result.Stderr, nil)
	}
	return nil
}

// Restart restarts the PostgreSQL service.
func (p *PostgreSQLEngine) Restart(ctx context.Context) error {
	result, err := p.exec.RunSudo(ctx, "systemctl", "restart", "postgresql")
	if err != nil {
		return fmt.Errorf("postgresql restart: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to restart postgresql: "+result.Stderr, nil)
	}
	return nil
}

// CreateDatabase creates a new PostgreSQL database.
func (p *PostgreSQLEngine) CreateDatabase(ctx context.Context, name, charset string) error {
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "createdb", name)
	if err != nil {
		return fmt.Errorf("postgresql create database: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to create database: "+result.Stderr, nil)
	}
	return nil
}

// DropDatabase drops a PostgreSQL database.
func (p *PostgreSQLEngine) DropDatabase(ctx context.Context, name string) error {
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "dropdb", "--if-exists", name)
	if err != nil {
		return fmt.Errorf("postgresql drop database: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to drop database: "+result.Stderr, nil)
	}
	return nil
}

// ListDatabases returns the list of non-system PostgreSQL databases.
func (p *PostgreSQLEngine) ListDatabases(ctx context.Context) ([]string, error) {
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-lqt")
	if err != nil {
		return nil, fmt.Errorf("postgresql list databases: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("POSTGRESQL_ERROR", "failed to list databases: "+result.Stderr, nil)
	}

	var databases []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 1 {
			continue
		}
		db := strings.TrimSpace(parts[0])
		if db == "" {
			continue
		}
		if pgSystemDBs[db] {
			continue
		}
		databases = append(databases, db)
	}
	return databases, nil
}

// CreateUser creates a new PostgreSQL user with a password.
func (p *PostgreSQLEngine) CreateUser(ctx context.Context, username, password string) error {
	stmt := fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", username, password)
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-c", stmt)
	if err != nil {
		return fmt.Errorf("postgresql create user: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to create user: "+result.Stderr, nil)
	}
	return nil
}

// DropUser drops a PostgreSQL user.
func (p *PostgreSQLEngine) DropUser(ctx context.Context, username string) error {
	stmt := fmt.Sprintf("DROP USER IF EXISTS %s", username)
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-c", stmt)
	if err != nil {
		return fmt.Errorf("postgresql drop user: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to drop user: "+result.Stderr, nil)
	}
	return nil
}

// ListUsers returns the list of non-system PostgreSQL users.
func (p *PostgreSQLEngine) ListUsers(ctx context.Context) ([]string, error) {
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-c", "\\du")
	if err != nil {
		return nil, fmt.Errorf("postgresql list users: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("POSTGRESQL_ERROR", "failed to list users: "+result.Stderr, nil)
	}

	var users []string
	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		// Only consider lines containing a pipe separator (actual data rows).
		if !strings.Contains(line, "|") {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		u := strings.TrimSpace(parts[0])
		if u == "" || strings.HasPrefix(u, "-") || strings.HasPrefix(u, "Role") || strings.HasPrefix(u, "(") {
			continue
		}
		if pgSystemUsers[u] {
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

// GrantPrivileges grants all privileges on a database to a user.
func (p *PostgreSQLEngine) GrantPrivileges(ctx context.Context, username, database string) error {
	stmt := fmt.Sprintf("GRANT ALL ON DATABASE %s TO %s", database, username)
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-c", stmt)
	if err != nil {
		return fmt.Errorf("postgresql grant privileges: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to grant privileges: "+result.Stderr, nil)
	}
	return nil
}

// ResetPassword changes the password for an existing PostgreSQL user.
func (p *PostgreSQLEngine) ResetPassword(ctx context.Context, username, password string) error {
	stmt := fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s'", username, password)
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-c", stmt)
	if err != nil {
		return fmt.Errorf("postgresql reset password: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to reset password: "+result.Stderr, nil)
	}
	return nil
}
