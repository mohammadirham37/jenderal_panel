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

// GrantPrivileges grants all privileges on a database to a user without
// transferring ownership: ALTER DATABASE ... OWNER TO would silently strip
// the previous owner's access every time the same database is granted to
// someone else. Since PostgreSQL 15 the public schema no longer grants
// CREATE to PUBLIC, so the explicit schema grant below is what keeps
// application migrations working. On modern versions the public schema is
// owned by pg_database_owner, which maps to the database owner; the schema
// grant also covers databases where it is not (restored dumps, older
// clusters).
func (p *PostgreSQLEngine) GrantPrivileges(ctx context.Context, username, database string) error {
	dbStmt := fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", database, username)
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-c", dbStmt)
	if err != nil {
		return fmt.Errorf("postgresql grant privileges: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to grant privileges: "+result.Stderr, nil)
	}

	schemaStmt := fmt.Sprintf("GRANT ALL ON SCHEMA public TO %s", username)
	result, err = p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-d", database, "-c", schemaStmt)
	if err != nil {
		return fmt.Errorf("postgresql grant schema privileges: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to grant schema privileges: "+result.Stderr, nil)
	}
	return nil
}

// RevokePrivileges removes every privilege a user holds on a database. If
// the user still owns the database (legacy grants transferred ownership),
// ownership moves back to postgres first — an owner cannot be stripped of
// their own database privileges.
func (p *PostgreSQLEngine) RevokePrivileges(ctx context.Context, username, database string) error {
	owner, err := p.databaseOwner(ctx, database)
	if err != nil {
		return err
	}
	if owner == username {
		transferStmt := fmt.Sprintf("ALTER DATABASE %s OWNER TO postgres", database)
		result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-c", transferStmt)
		if err != nil {
			return fmt.Errorf("postgresql revoke privileges: %w", err)
		}
		if result.ExitCode != 0 {
			return model.NewDomainError("POSTGRESQL_ERROR", "failed to transfer database ownership: "+result.Stderr, nil)
		}
	}

	revokeStmt := fmt.Sprintf(
		"REVOKE ALL PRIVILEGES ON DATABASE %s FROM %s; "+
			"REVOKE ALL ON SCHEMA public FROM %s; "+
			"REVOKE ALL ON ALL TABLES IN SCHEMA public FROM %s; "+
			"REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM %s",
		database, username, username, username, username)
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-d", database, "-c", revokeStmt)
	if err != nil {
		return fmt.Errorf("postgresql revoke privileges: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("POSTGRESQL_ERROR", "failed to revoke privileges: "+result.Stderr, nil)
	}
	return nil
}

// databaseOwner returns the role name that owns a database.
func (p *PostgreSQLEngine) databaseOwner(ctx context.Context, database string) (string, error) {
	stmt := fmt.Sprintf("SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = '%s'", database)
	result, err := p.exec.RunSudo(ctx, "sudo", "-u", "postgres", "psql", "-tAc", stmt)
	if err != nil {
		return "", fmt.Errorf("postgresql database owner: %w", err)
	}
	if result.ExitCode != 0 {
		return "", model.NewDomainError("POSTGRESQL_ERROR", "failed to look up database owner: "+result.Stderr, nil)
	}
	return strings.TrimSpace(result.Stdout), nil
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
