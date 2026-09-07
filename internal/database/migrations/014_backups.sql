CREATE TABLE IF NOT EXISTS backups (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    target      TEXT,
    storage     TEXT NOT NULL DEFAULT 'local',
    path        TEXT NOT NULL,
    size_bytes  INTEGER,
    status      TEXT NOT NULL DEFAULT 'pending',
    error_msg   TEXT,
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS backup_schedules (
    id             TEXT PRIMARY KEY,
    type           TEXT NOT NULL,
    target         TEXT,
    storage        TEXT NOT NULL DEFAULT 'local',
    schedule       TEXT NOT NULL,
    retention_days INTEGER NOT NULL DEFAULT 7,
    enabled        INTEGER NOT NULL DEFAULT 1,
    last_run       TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_backups_type ON backups(type);
CREATE INDEX IF NOT EXISTS idx_backups_created_at ON backups(created_at);
