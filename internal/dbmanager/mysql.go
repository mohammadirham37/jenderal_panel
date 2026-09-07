package dbmanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// mysqlSystemDBs are databases that should be filtered from listing results.
var mysqlSystemDBs = map[string]bool{
	"information_schema": true,
	"mysql":              true,
	"performance_schema": true,
	"sys":                true,
}

// mysqlSystemUsers are users that should be filtered from listing results.
var mysqlSystemUsers = map[string]bool{
	"root":             true,
	"mysql.sys":        true,
	"mysql.session":    true,
	"mysql.infoschema": true,
	"debian-sys-maint": true,
}

// MySQLEngine implements DatabaseEngine for MySQL.
type MySQLEngine struct {
	exec executor.CommandExecutor
}

// NewMySQLEngine creates a new MySQL engine backed by the given executor.
func NewMySQLEngine(exec executor.CommandExecutor) *MySQLEngine {
	return &MySQLEngine{exec: exec}
}

// Install installs MySQL server via apt-get.
func (m *MySQLEngine) Install(ctx context.Context) error {
	result, err := m.exec.RunSudo(ctx, "apt-get", "install", "-y", "mysql-server")
	if err != nil {
		return fmt.Errorf("mysql install: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to install mysql: "+result.Stderr, nil)
	}
	return nil
}

// Status returns the current status of MySQL including whether it is
// installed, running, and its version string.
func (m *MySQLEngine) Status(ctx context.Context) (model.EngineStatus, error) {
	status := model.EngineStatus{Name: "mysql"}

	// Check if installed by running mysql --version.
	verResult, err := m.exec.Run(ctx, "mysql", "--version")
	if err != nil {
		return status, nil // not installed
	}
	if verResult.ExitCode != 0 {
		return status, nil // not installed
	}
	status.Installed = true
	status.Version = strings.TrimSpace(verResult.Stdout)

	// Check if running via systemctl.
	sysResult, err := m.exec.RunSudo(ctx, "systemctl", "is-active", "mysql")
	if err != nil {
		return status, nil
	}
	status.Running = strings.TrimSpace(sysResult.Stdout) == "active"

	return status, nil
}

// Start starts the MySQL service.
func (m *MySQLEngine) Start(ctx context.Context) error {
	result, err := m.exec.RunSudo(ctx, "systemctl", "start", "mysql")
	if err != nil {
		return fmt.Errorf("mysql start: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to start mysql: "+result.Stderr, nil)
	}
	return nil
}

// Stop stops the MySQL service.
func (m *MySQLEngine) Stop(ctx context.Context) error {
	result, err := m.exec.RunSudo(ctx, "systemctl", "stop", "mysql")
	if err != nil {
		return fmt.Errorf("mysql stop: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to stop mysql: "+result.Stderr, nil)
	}
	return nil
}

// Restart restarts the MySQL service.
func (m *MySQLEngine) Restart(ctx context.Context) error {
	result, err := m.exec.RunSudo(ctx, "systemctl", "restart", "mysql")
	if err != nil {
		return fmt.Errorf("mysql restart: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to restart mysql: "+result.Stderr, nil)
	}
	return nil
}

// CreateDatabase creates a new MySQL database with the given charset.
func (m *MySQLEngine) CreateDatabase(ctx context.Context, name, charset string) error {
	if charset == "" {
		charset = "utf8mb4"
	}
	stmt := fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET %s", name, charset)
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", stmt)
	if err != nil {
		return fmt.Errorf("mysql create database: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to create database: "+result.Stderr, nil)
	}
	return nil
}

// DropDatabase drops a MySQL database.
func (m *MySQLEngine) DropDatabase(ctx context.Context, name string) error {
	stmt := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name)
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", stmt)
	if err != nil {
		return fmt.Errorf("mysql drop database: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to drop database: "+result.Stderr, nil)
	}
	return nil
}

// ListDatabases returns the list of non-system MySQL databases.
func (m *MySQLEngine) ListDatabases(ctx context.Context) ([]string, error) {
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", "SHOW DATABASES")
	if err != nil {
		return nil, fmt.Errorf("mysql list databases: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("MYSQL_ERROR", "failed to list databases: "+result.Stderr, nil)
	}

	var databases []string
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		db := strings.TrimSpace(line)
		if db == "" || db == "Database" {
			continue
		}
		if mysqlSystemDBs[db] {
			continue
		}
		databases = append(databases, db)
	}
	return databases, nil
}

// CreateUser creates a new MySQL user identified by password.
func (m *MySQLEngine) CreateUser(ctx context.Context, username, password string) error {
	stmt := fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s'", username, password)
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", stmt)
	if err != nil {
		return fmt.Errorf("mysql create user: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to create user: "+result.Stderr, nil)
	}
	return nil
}

// DropUser drops a MySQL user.
func (m *MySQLEngine) DropUser(ctx context.Context, username string) error {
	stmt := fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost'", username)
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", stmt)
	if err != nil {
		return fmt.Errorf("mysql drop user: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to drop user: "+result.Stderr, nil)
	}
	return nil
}

// ListUsers returns the list of non-system MySQL users.
func (m *MySQLEngine) ListUsers(ctx context.Context) ([]string, error) {
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", "SELECT User FROM mysql.user")
	if err != nil {
		return nil, fmt.Errorf("mysql list users: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, model.NewDomainError("MYSQL_ERROR", "failed to list users: "+result.Stderr, nil)
	}

	var users []string
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		u := strings.TrimSpace(line)
		if u == "" || u == "User" {
			continue
		}
		if mysqlSystemUsers[u] {
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

// GrantPrivileges grants all privileges on a database to a user.
func (m *MySQLEngine) GrantPrivileges(ctx context.Context, username, database string) error {
	stmt := fmt.Sprintf("GRANT ALL ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES", database, username)
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", stmt)
	if err != nil {
		return fmt.Errorf("mysql grant privileges: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to grant privileges: "+result.Stderr, nil)
	}
	return nil
}

// ResetPassword changes the password for an existing MySQL user.
func (m *MySQLEngine) ResetPassword(ctx context.Context, username, password string) error {
	stmt := fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s'", username, password)
	result, err := m.exec.RunSudo(ctx, "mysql", "-e", stmt)
	if err != nil {
		return fmt.Errorf("mysql reset password: %w", err)
	}
	if result.ExitCode != 0 {
		return model.NewDomainError("MYSQL_ERROR", "failed to reset password: "+result.Stderr, nil)
	}
	return nil
}
