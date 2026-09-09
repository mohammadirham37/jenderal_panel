CREATE TABLE IF NOT EXISTS background_tasks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    module TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK(status IN ('running', 'completed', 'failed')),
    output TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    ended_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_background_tasks_updated
    ON background_tasks(updated_at DESC);
