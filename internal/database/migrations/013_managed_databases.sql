CREATE TABLE IF NOT EXISTS managed_databases (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    engine     TEXT NOT NULL,
    charset    TEXT DEFAULT 'utf8mb4',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS db_users (
    id          TEXT PRIMARY KEY,
    username    TEXT NOT NULL,
    engine      TEXT NOT NULL,
    privileges  TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE(username, engine)
);

CREATE INDEX IF NOT EXISTS idx_managed_databases_engine ON managed_databases(engine);
CREATE INDEX IF NOT EXISTS idx_db_users_engine ON db_users(engine);
