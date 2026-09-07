CREATE TABLE IF NOT EXISTS queue_workers (
    id           TEXT PRIMARY KEY,
    website_id   TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    command      TEXT NOT NULL,
    num_workers  INTEGER NOT NULL DEFAULT 1,
    auto_restart INTEGER NOT NULL DEFAULT 1,
    status       TEXT NOT NULL DEFAULT 'stopped',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_queue_workers_website_id ON queue_workers(website_id);
