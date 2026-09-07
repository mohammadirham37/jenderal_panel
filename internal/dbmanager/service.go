package dbmanager

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// Service orchestrates database engine operations and persists metadata
// about managed databases and database users in the panel's own SQLite DB.
type Service struct {
	db      *sql.DB
	exec    executor.CommandExecutor
	audit   *audit.Service
	engines map[string]DatabaseEngine
}

// NewService creates a new Service with MySQL, PostgreSQL, and Redis engines.
func NewService(db *sql.DB, exec executor.CommandExecutor, auditSvc *audit.Service) *Service {
	engines := map[string]DatabaseEngine{
		"mysql":      NewMySQLEngine(exec),
		"postgresql": NewPostgreSQLEngine(exec),
		"redis":      NewRedisEngine(exec),
	}
	return &Service{
		db:      db,
		exec:    exec,
		audit:   auditSvc,
		engines: engines,
	}
}

// engine returns the DatabaseEngine for the given name, or a validation error.
func (s *Service) engine(name string) (DatabaseEngine, error) {
	e, ok := s.engines[name]
	if !ok {
		return nil, model.NewValidationError(fmt.Sprintf("unsupported engine: %s", name))
	}
	return e, nil
}

// ListEngines returns the status of all registered database engines.
func (s *Service) ListEngines(ctx context.Context) ([]model.EngineStatus, error) {
	var statuses []model.EngineStatus
	for _, name := range []string{"mysql", "postgresql", "redis"} {
		eng := s.engines[name]
		st, err := eng.Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("status %s: %w", name, err)
		}
		statuses = append(statuses, st)
	}
	return statuses, nil
}

// InstallEngine installs the given database engine.
func (s *Service) InstallEngine(ctx context.Context, engineName string) error {
	eng, err := s.engine(engineName)
	if err != nil {
		return err
	}
	return eng.Install(ctx)
}

// StartEngine starts the given database engine service.
func (s *Service) StartEngine(ctx context.Context, engineName string) error {
	eng, err := s.engine(engineName)
	if err != nil {
		return err
	}
	return eng.Start(ctx)
}

// StopEngine stops the given database engine service.
func (s *Service) StopEngine(ctx context.Context, engineName string) error {
	eng, err := s.engine(engineName)
	if err != nil {
		return err
	}
	return eng.Stop(ctx)
}

// RestartEngine restarts the given database engine service.
func (s *Service) RestartEngine(ctx context.Context, engineName string) error {
	eng, err := s.engine(engineName)
	if err != nil {
		return err
	}
	return eng.Restart(ctx)
}

// CreateDatabase creates a database on the engine and records it in the panel DB.
func (s *Service) CreateDatabase(ctx context.Context, name, engineName, charset string) (model.ManagedDatabase, error) {
	if name == "" {
		return model.ManagedDatabase{}, model.NewValidationError("database name is required")
	}
	if engineName == "" {
		return model.ManagedDatabase{}, model.NewValidationError("engine is required")
	}

	eng, err := s.engine(engineName)
	if err != nil {
		return model.ManagedDatabase{}, err
	}

	if charset == "" {
		charset = "utf8mb4"
	}

	if err := eng.CreateDatabase(ctx, name, charset); err != nil {
		return model.ManagedDatabase{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	id := ulid.Make().String()

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO managed_databases (id, name, engine, charset, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, engineName, charset, now, now,
	)
	if err != nil {
		return model.ManagedDatabase{}, fmt.Errorf("insert managed database: %w", err)
	}

	mdb := model.ManagedDatabase{
		ID:      id,
		Name:    name,
		Engine:  engineName,
		Charset: charset,
	}
	mdb.CreatedAt, _ = time.Parse(time.RFC3339, now)
	mdb.UpdatedAt = mdb.CreatedAt

	return mdb, nil
}

// DropDatabase drops a database from the engine and removes the panel record.
func (s *Service) DropDatabase(ctx context.Context, id string) error {
	var name, engineName string
	err := s.db.QueryRowContext(ctx,
		`SELECT name, engine FROM managed_databases WHERE id = ?`, id,
	).Scan(&name, &engineName)
	if err == sql.ErrNoRows {
		return model.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query managed database: %w", err)
	}

	eng, err := s.engine(engineName)
	if err != nil {
		return err
	}

	if err := eng.DropDatabase(ctx, name); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM managed_databases WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete managed database: %w", err)
	}

	return nil
}

// ListDatabases returns all managed databases from the panel DB.
func (s *Service) ListDatabases(ctx context.Context) ([]model.ManagedDatabase, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, engine, charset, created_at, updated_at
		 FROM managed_databases ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query managed databases: %w", err)
	}
	defer rows.Close()

	var dbs []model.ManagedDatabase
	for rows.Next() {
		var d model.ManagedDatabase
		var createdStr, updatedStr string
		if err := rows.Scan(&d.ID, &d.Name, &d.Engine, &d.Charset, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan managed database: %w", err)
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		d.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		dbs = append(dbs, d)
	}

	return dbs, rows.Err()
}

// CreateDBUser creates a user on the engine and records it in the panel DB.
func (s *Service) CreateDBUser(ctx context.Context, username, password, engineName string) (model.DBUser, error) {
	if username == "" {
		return model.DBUser{}, model.NewValidationError("username is required")
	}
	if password == "" {
		return model.DBUser{}, model.NewValidationError("password is required")
	}
	if engineName == "" {
		return model.DBUser{}, model.NewValidationError("engine is required")
	}

	eng, err := s.engine(engineName)
	if err != nil {
		return model.DBUser{}, err
	}

	if err := eng.CreateUser(ctx, username, password); err != nil {
		return model.DBUser{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	id := ulid.Make().String()

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO db_users (id, username, engine, privileges, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, username, engineName, "[]", now, now,
	)
	if err != nil {
		return model.DBUser{}, fmt.Errorf("insert db user: %w", err)
	}

	u := model.DBUser{
		ID:         id,
		Username:   username,
		Engine:     engineName,
		Privileges: "[]",
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, now)
	u.UpdatedAt = u.CreatedAt

	return u, nil
}

// DropDBUser drops a user from the engine and removes the panel record.
func (s *Service) DropDBUser(ctx context.Context, id string) error {
	var username, engineName string
	err := s.db.QueryRowContext(ctx,
		`SELECT username, engine FROM db_users WHERE id = ?`, id,
	).Scan(&username, &engineName)
	if err == sql.ErrNoRows {
		return model.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query db user: %w", err)
	}

	eng, err := s.engine(engineName)
	if err != nil {
		return err
	}

	if err := eng.DropUser(ctx, username); err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `DELETE FROM db_users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete db user: %w", err)
	}

	return nil
}

// ListDBUsers returns all database users from the panel DB.
func (s *Service) ListDBUsers(ctx context.Context) ([]model.DBUser, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, engine, privileges, created_at, updated_at
		 FROM db_users ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query db users: %w", err)
	}
	defer rows.Close()

	var users []model.DBUser
	for rows.Next() {
		var u model.DBUser
		var privs sql.NullString
		var createdStr, updatedStr string
		if err := rows.Scan(&u.ID, &u.Username, &u.Engine, &privs, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan db user: %w", err)
		}
		u.Privileges = privs.String
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		users = append(users, u)
	}

	return users, rows.Err()
}

// ResetPassword resets the password for a database user on the engine.
func (s *Service) ResetPassword(ctx context.Context, id, password string) error {
	if password == "" {
		return model.NewValidationError("password is required")
	}

	var username, engineName string
	err := s.db.QueryRowContext(ctx,
		`SELECT username, engine FROM db_users WHERE id = ?`, id,
	).Scan(&username, &engineName)
	if err == sql.ErrNoRows {
		return model.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query db user: %w", err)
	}

	eng, err := s.engine(engineName)
	if err != nil {
		return err
	}

	return eng.ResetPassword(ctx, username, password)
}

// GrantPrivileges grants all privileges on the specified database to the
// specified user. Both must exist in the panel DB and share the same engine.
func (s *Service) GrantPrivileges(ctx context.Context, userID, databaseID string) error {
	var username, userEngine string
	err := s.db.QueryRowContext(ctx,
		`SELECT username, engine FROM db_users WHERE id = ?`, userID,
	).Scan(&username, &userEngine)
	if err == sql.ErrNoRows {
		return model.NewValidationError("user not found")
	}
	if err != nil {
		return fmt.Errorf("query db user: %w", err)
	}

	var dbName, dbEngine string
	err = s.db.QueryRowContext(ctx,
		`SELECT name, engine FROM managed_databases WHERE id = ?`, databaseID,
	).Scan(&dbName, &dbEngine)
	if err == sql.ErrNoRows {
		return model.NewValidationError("database not found")
	}
	if err != nil {
		return fmt.Errorf("query managed database: %w", err)
	}

	if userEngine != dbEngine {
		return model.NewValidationError("user and database must use the same engine")
	}

	eng, err := s.engine(userEngine)
	if err != nil {
		return err
	}

	return eng.GrantPrivileges(ctx, username, dbName)
}

// Count returns the total number of managed databases.
func (s *Service) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_databases`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count managed databases: %w", err)
	}
	return count, nil
}
