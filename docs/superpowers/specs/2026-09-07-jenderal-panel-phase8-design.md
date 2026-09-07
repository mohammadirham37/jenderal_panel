# Jenderal Panel — Phase 8 (Backup) Design Spec

## Overview

Local and S3-compatible backup for websites, databases, and configs. Scheduled backups with retention policies.

## 1. Database Schema

```sql
-- 014_backups.sql
CREATE TABLE IF NOT EXISTS backups (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,          -- website, database, config, full
    target      TEXT,                   -- website domain or database name
    storage     TEXT NOT NULL DEFAULT 'local',  -- local, s3
    path        TEXT NOT NULL,
    size_bytes  INTEGER,
    status      TEXT NOT NULL DEFAULT 'pending',  -- pending, running, completed, failed
    error_msg   TEXT,
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS backup_schedules (
    id             TEXT PRIMARY KEY,
    type           TEXT NOT NULL,
    target         TEXT,
    storage        TEXT NOT NULL DEFAULT 'local',
    schedule       TEXT NOT NULL,       -- cron expression
    retention_days INTEGER NOT NULL DEFAULT 7,
    enabled        INTEGER NOT NULL DEFAULT 1,
    last_run       TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_backups_type ON backups(type);
CREATE INDEX IF NOT EXISTS idx_backups_created_at ON backups(created_at);
```

## 2. Service

```go
type Service struct {
    db        *sql.DB
    exec      executor.CommandExecutor
    audit     *audit.Service
    localDir  string  // /var/lib/jenderal/backups/
}
```

### Backup Operations
- BackupWebsite(ctx, domain) — tar -czf website home directory
- BackupDatabase(ctx, name, engine) — mysqldump / pg_dump
- BackupConfig(ctx) — tar /etc/jenderal/ + /etc/nginx/
- BackupFull(ctx) — all websites + all databases + config
- Restore(ctx, backupID) — extract/restore based on type
- Delete(ctx, id) — remove file + DB record
- List(ctx, filters) — paginated
- Get(ctx, id)

### Schedule Operations
- CreateSchedule / UpdateSchedule / DeleteSchedule
- ListSchedules
- EnableSchedule / DisableSchedule

### Background Worker
- Goroutine checks schedules every hour
- Run matching schedules
- Cleanup old backups based on retention_days

### Storage
- Local: /var/lib/jenderal/backups/{type}/{timestamp}_{target}.tar.gz
- S3: future — interface ready but local-only for now

## 3. API Routes

```
POST   /api/v1/backups                → create backup {type, target}
GET    /api/v1/backups                → list
GET    /api/v1/backups/{id}           → get
DELETE /api/v1/backups/{id}           → delete
POST   /api/v1/backups/{id}/restore   → restore

GET    /api/v1/backup-schedules
POST   /api/v1/backup-schedules
PUT    /api/v1/backup-schedules/{id}
DELETE /api/v1/backup-schedules/{id}
POST   /api/v1/backup-schedules/{id}/enable
POST   /api/v1/backup-schedules/{id}/disable
```

## 4. RBAC
`backups.view`, `backups.create`, `backups.restore`, `backups.delete`

## 5. Frontend
`/backups` — backup list, create form (type + target select), schedules table.

## 6. File Structure
```
internal/backup/
├── service.go
├── service_test.go
├── scheduler.go
└── handler.go
```
