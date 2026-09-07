package dbmanager

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/audit"
	"github.com/mohammadirham37/jenderal_panel/internal/database"
	"github.com/mohammadirham37/jenderal_panel/internal/executor"
	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

// okResult returns a successful executor result with the given stdout.
func okResult(stdout string) *executor.Result {
	return &executor.Result{Stdout: stdout, ExitCode: 0}
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newMockExec() *executor.MockExecutor {
	return &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return okResult(""), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return okResult(""), nil
		},
	}
}

func newTestService(t *testing.T, mock *executor.MockExecutor) *Service {
	t.Helper()
	db := setupTestDB(t)
	auditSvc := audit.NewService(db)
	return NewService(db, mock, auditSvc)
}

func TestCreateDatabase(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	mdb, err := svc.CreateDatabase(ctx, "testdb", "mysql", "utf8mb4")
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	if mdb.ID == "" {
		t.Error("expected non-empty ID")
	}
	if mdb.Name != "testdb" {
		t.Errorf("name: want testdb, got %s", mdb.Name)
	}
	if mdb.Engine != "mysql" {
		t.Errorf("engine: want mysql, got %s", mdb.Engine)
	}
	if mdb.Charset != "utf8mb4" {
		t.Errorf("charset: want utf8mb4, got %s", mdb.Charset)
	}
	if mdb.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}

	// Verify the record exists in the DB.
	dbs, err := svc.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(dbs) != 1 {
		t.Fatalf("expected 1 database, got %d", len(dbs))
	}
	if dbs[0].ID != mdb.ID {
		t.Errorf("listed ID: want %s, got %s", mdb.ID, dbs[0].ID)
	}
}

func TestCreateDatabase_Validation(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	_, err := svc.CreateDatabase(ctx, "", "mysql", "utf8mb4")
	if err == nil {
		t.Fatal("expected validation error for empty name")
	}
	domainErr, ok := err.(*model.DomainError)
	if !ok {
		t.Fatalf("expected *model.DomainError, got %T", err)
	}
	if domainErr.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", domainErr.Code)
	}

	_, err = svc.CreateDatabase(ctx, "testdb", "badengine", "utf8mb4")
	if err == nil {
		t.Fatal("expected validation error for bad engine")
	}
}

func TestListDatabases(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	_, err := svc.CreateDatabase(ctx, "db1", "mysql", "utf8mb4")
	if err != nil {
		t.Fatalf("CreateDatabase db1: %v", err)
	}
	_, err = svc.CreateDatabase(ctx, "db2", "postgresql", "utf8")
	if err != nil {
		t.Fatalf("CreateDatabase db2: %v", err)
	}

	dbs, err := svc.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases, got %d", len(dbs))
	}

	count, err := svc.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("Count: want 2, got %d", count)
	}
}

func TestDropDatabase(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	mdb, err := svc.CreateDatabase(ctx, "dropme", "mysql", "utf8mb4")
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	if err := svc.DropDatabase(ctx, mdb.ID); err != nil {
		t.Fatalf("DropDatabase: %v", err)
	}

	dbs, err := svc.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(dbs) != 0 {
		t.Errorf("expected 0 databases after drop, got %d", len(dbs))
	}
}

func TestDropDatabase_NotFound(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	err := svc.DropDatabase(ctx, "nonexistent")
	if err != model.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateDBUser(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	u, err := svc.CreateDBUser(ctx, "testuser", "secret123", "mysql")
	if err != nil {
		t.Fatalf("CreateDBUser: %v", err)
	}

	if u.ID == "" {
		t.Error("expected non-empty ID")
	}
	if u.Username != "testuser" {
		t.Errorf("username: want testuser, got %s", u.Username)
	}
	if u.Engine != "mysql" {
		t.Errorf("engine: want mysql, got %s", u.Engine)
	}
	if u.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}

	// Verify the record exists.
	users, err := svc.ListDBUsers(ctx)
	if err != nil {
		t.Fatalf("ListDBUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].ID != u.ID {
		t.Errorf("listed ID: want %s, got %s", u.ID, users[0].ID)
	}
}

func TestCreateDBUser_Validation(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	_, err := svc.CreateDBUser(ctx, "", "pass", "mysql")
	if err == nil {
		t.Fatal("expected validation error for empty username")
	}

	_, err = svc.CreateDBUser(ctx, "user", "", "mysql")
	if err == nil {
		t.Fatal("expected validation error for empty password")
	}

	_, err = svc.CreateDBUser(ctx, "user", "pass", "")
	if err == nil {
		t.Fatal("expected validation error for empty engine")
	}
}

func TestDropDBUser(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	u, err := svc.CreateDBUser(ctx, "dropme", "pass123", "postgresql")
	if err != nil {
		t.Fatalf("CreateDBUser: %v", err)
	}

	if err := svc.DropDBUser(ctx, u.ID); err != nil {
		t.Fatalf("DropDBUser: %v", err)
	}

	users, err := svc.ListDBUsers(ctx)
	if err != nil {
		t.Fatalf("ListDBUsers: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users after drop, got %d", len(users))
	}
}

func TestDropDBUser_NotFound(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	err := svc.DropDBUser(ctx, "nonexistent")
	if err != model.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestResetPassword(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	u, err := svc.CreateDBUser(ctx, "resetme", "oldpass", "mysql")
	if err != nil {
		t.Fatalf("CreateDBUser: %v", err)
	}

	if err := svc.ResetPassword(ctx, u.ID, "newpass"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
}

func TestResetPassword_NotFound(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	err := svc.ResetPassword(ctx, "nonexistent", "pass")
	if err != model.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGrantPrivileges(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	u, err := svc.CreateDBUser(ctx, "grantme", "pass", "mysql")
	if err != nil {
		t.Fatalf("CreateDBUser: %v", err)
	}

	mdb, err := svc.CreateDatabase(ctx, "grantdb", "mysql", "utf8mb4")
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	if err := svc.GrantPrivileges(ctx, u.ID, mdb.ID); err != nil {
		t.Fatalf("GrantPrivileges: %v", err)
	}
}

func TestGrantPrivileges_EngineMismatch(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	u, err := svc.CreateDBUser(ctx, "mysqluser", "pass", "mysql")
	if err != nil {
		t.Fatalf("CreateDBUser: %v", err)
	}

	mdb, err := svc.CreateDatabase(ctx, "pgdb", "postgresql", "utf8")
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	err = svc.GrantPrivileges(ctx, u.ID, mdb.ID)
	if err == nil {
		t.Fatal("expected error for engine mismatch")
	}
	domainErr, ok := err.(*model.DomainError)
	if !ok {
		t.Fatalf("expected *model.DomainError, got %T", err)
	}
	if domainErr.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", domainErr.Code)
	}
}

func TestListEngines(t *testing.T) {
	mock := &executor.MockExecutor{
		RunFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			switch name {
			case "mysql":
				return &executor.Result{Stdout: "mysql  Ver 8.0.36\n", ExitCode: 0}, nil
			case "psql":
				return &executor.Result{Stdout: "psql (PostgreSQL) 16.2\n", ExitCode: 0}, nil
			case "redis-cli":
				return &executor.Result{Stdout: "redis-cli 7.0.15\n", ExitCode: 0}, nil
			}
			return okResult(""), nil
		},
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			// systemctl is-active returns "active"
			if name == "systemctl" && len(args) > 0 && args[0] == "is-active" {
				return &executor.Result{Stdout: "active\n", ExitCode: 0}, nil
			}
			return okResult(""), nil
		},
	}

	db := setupTestDB(t)
	auditSvc := audit.NewService(db)
	svc := NewService(db, mock, auditSvc)
	ctx := context.Background()

	statuses, err := svc.ListEngines(ctx)
	if err != nil {
		t.Fatalf("ListEngines: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("expected 3 engine statuses, got %d", len(statuses))
	}

	// Check MySQL
	if statuses[0].Name != "mysql" {
		t.Errorf("engine[0] name: want mysql, got %s", statuses[0].Name)
	}
	if !statuses[0].Installed {
		t.Error("expected mysql installed")
	}
	if !statuses[0].Running {
		t.Error("expected mysql running")
	}
	if statuses[0].Version == "" {
		t.Error("expected non-empty mysql version")
	}

	// Check PostgreSQL
	if statuses[1].Name != "postgresql" {
		t.Errorf("engine[1] name: want postgresql, got %s", statuses[1].Name)
	}
	if !statuses[1].Installed {
		t.Error("expected postgresql installed")
	}
	if !statuses[1].Running {
		t.Error("expected postgresql running")
	}

	// Check Redis
	if statuses[2].Name != "redis" {
		t.Errorf("engine[2] name: want redis, got %s", statuses[2].Name)
	}
	if !statuses[2].Installed {
		t.Error("expected redis installed")
	}
	if !statuses[2].Running {
		t.Error("expected redis running")
	}
}

func TestCount_Empty(t *testing.T) {
	mock := newMockExec()
	svc := newTestService(t, mock)
	ctx := context.Background()

	count, err := svc.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestRedisUnsupportedOps(t *testing.T) {
	redis := NewRedisEngine(newMockExec())
	ctx := context.Background()

	if err := redis.CreateDatabase(ctx, "db", "utf8"); err == nil {
		t.Error("expected error for CreateDatabase")
	}
	if err := redis.DropDatabase(ctx, "db"); err == nil {
		t.Error("expected error for DropDatabase")
	}
	if _, err := redis.ListDatabases(ctx); err == nil {
		t.Error("expected error for ListDatabases")
	}
	if err := redis.CreateUser(ctx, "u", "p"); err == nil {
		t.Error("expected error for CreateUser")
	}
	if err := redis.DropUser(ctx, "u"); err == nil {
		t.Error("expected error for DropUser")
	}
	if _, err := redis.ListUsers(ctx); err == nil {
		t.Error("expected error for ListUsers")
	}
	if err := redis.GrantPrivileges(ctx, "u", "db"); err == nil {
		t.Error("expected error for GrantPrivileges")
	}
	if err := redis.ResetPassword(ctx, "u", "p"); err == nil {
		t.Error("expected error for ResetPassword")
	}
}

func TestMySQLListDatabases(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stdout:   "Database\ninformation_schema\nmysql\nperformance_schema\nsys\nmyapp\ntestdb\n",
				ExitCode: 0,
			}, nil
		},
	}

	engine := NewMySQLEngine(mock)
	dbs, err := engine.ListDatabases(context.Background())
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases, got %d: %v", len(dbs), dbs)
	}
	if dbs[0] != "myapp" || dbs[1] != "testdb" {
		t.Errorf("unexpected databases: %v", dbs)
	}
}

func TestMySQLListUsers(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stdout:   "User\nroot\nmysql.sys\nmysql.session\nmysql.infoschema\ndebian-sys-maint\nappuser\n",
				ExitCode: 0,
			}, nil
		},
	}

	engine := NewMySQLEngine(mock)
	users, err := engine.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d: %v", len(users), users)
	}
	if users[0] != "appuser" {
		t.Errorf("expected appuser, got %s", users[0])
	}
}

func TestPostgreSQLListDatabases(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stdout:   " postgres  | postgres | UTF8\n template0 | postgres | UTF8\n template1 | postgres | UTF8\n myappdb   | appuser  | UTF8\n",
				ExitCode: 0,
			}, nil
		},
	}

	engine := NewPostgreSQLEngine(mock)
	dbs, err := engine.ListDatabases(context.Background())
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(dbs) != 1 {
		t.Fatalf("expected 1 database, got %d: %v", len(dbs), dbs)
	}
	if dbs[0] != "myappdb" {
		t.Errorf("expected myappdb, got %s", dbs[0])
	}
}

func TestPostgreSQLListUsers(t *testing.T) {
	mock := &executor.MockExecutor{
		RunSudoFunc: func(ctx context.Context, name string, args ...string) (*executor.Result, error) {
			return &executor.Result{
				Stdout:   "                             List of roles\n Role name |                         Attributes\n-----------+--------------------------------------------\n postgres  | Superuser, Create role, Create DB, Replication\n appuser   | \n",
				ExitCode: 0,
			}, nil
		},
	}

	engine := NewPostgreSQLEngine(mock)
	users, err := engine.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d: %v", len(users), users)
	}
	if users[0] != "appuser" {
		t.Errorf("expected appuser, got %s", users[0])
	}
}
