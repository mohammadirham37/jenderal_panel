# Jenderal Panel — Phase 6 (Database Management) Design Spec

## Overview

Install, manage, and administer MySQL/MariaDB, PostgreSQL, and Redis through the panel.

## 1. Database Schema

```sql
-- 013_managed_databases.sql
CREATE TABLE IF NOT EXISTS managed_databases (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    engine     TEXT NOT NULL,          -- mysql, postgresql, redis
    charset    TEXT DEFAULT 'utf8mb4',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS db_users (
    id          TEXT PRIMARY KEY,
    username    TEXT NOT NULL,
    engine      TEXT NOT NULL,
    privileges  TEXT,                   -- JSON: databases + access levels
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE(username, engine)
);
```

## 2. Services

### MySQL Service (`internal/dbmanager/mysql.go`)
- Install(ctx) — apt-get install mysql-server
- Status(ctx) — systemctl status mysql
- Start/Stop/Restart(ctx)
- CreateDatabase(ctx, name, charset)
- DropDatabase(ctx, name)
- ListDatabases(ctx) — mysql -e "SHOW DATABASES"
- CreateUser(ctx, username, password, host)
- DropUser(ctx, username)
- ListUsers(ctx)
- GrantPrivileges(ctx, username, database, privileges)
- ResetPassword(ctx, username, password)

### PostgreSQL Service (`internal/dbmanager/postgresql.go`)
- Same operations via psql/createdb/createuser commands

### Redis Service (`internal/dbmanager/redis.go`)
- Install/Status/Start/Stop/Restart
- GetConfig/SetConfig (maxmemory, etc)
- FlushAll/FlushDB (with confirm)
- Info(ctx) — redis-cli INFO

### Engine Interface
```go
type DatabaseEngine interface {
    Install(ctx) error
    Status(ctx) (EngineStatus, error)
    Start/Stop/Restart(ctx) error
    CreateDatabase(ctx, name, charset) error
    DropDatabase(ctx, name) error
    ListDatabases(ctx) ([]string, error)
    CreateUser(ctx, username, password) error
    DropUser(ctx, username) error
    ListUsers(ctx) ([]string, error)
    GrantPrivileges(ctx, username, database) error
}
```

## 3. API Routes

```
# Engine management
GET    /api/v1/databases/engines              → list engines + status
POST   /api/v1/databases/engines/{engine}/install
POST   /api/v1/databases/engines/{engine}/start
POST   /api/v1/databases/engines/{engine}/stop
POST   /api/v1/databases/engines/{engine}/restart

# Databases
GET    /api/v1/databases
POST   /api/v1/databases
DELETE /api/v1/databases/{id}

# Database Users
GET    /api/v1/databases/users
POST   /api/v1/databases/users
DELETE /api/v1/databases/users/{id}
POST   /api/v1/databases/users/{id}/password
POST   /api/v1/databases/users/{id}/grant
```

## 4. RBAC
`databases.view`, `databases.create`, `databases.delete`, `databases.users`

## 5. Frontend
`/databases` — engine status cards, databases table, users table, create forms.

## 6. File Structure
```
internal/dbmanager/
├── engine.go          # Interface + EngineStatus struct
├── mysql.go           # MySQL implementation
├── mysql_test.go
├── postgresql.go      # PostgreSQL implementation
├── postgresql_test.go
├── redis.go           # Redis implementation
├── redis_test.go
├── service.go         # Service orchestrator (routes to correct engine)
├── service_test.go
└── handler.go         # HTTP handlers
```
