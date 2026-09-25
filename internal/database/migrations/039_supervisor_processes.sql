CREATE TABLE IF NOT EXISTS supervisor_processes (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    command      TEXT NOT NULL,
    working_dir  TEXT NOT NULL DEFAULT '',
    run_as       TEXT NOT NULL,
    env          TEXT NOT NULL DEFAULT '',
    auto_restart INTEGER NOT NULL DEFAULT 1,
    status       TEXT NOT NULL DEFAULT 'stopped',
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
