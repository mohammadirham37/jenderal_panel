-- Daily disk usage snapshots so the Disk page can answer "what grew?"
-- instead of only "what is big right now".
CREATE TABLE IF NOT EXISTS disk_usage_history (
    day         TEXT PRIMARY KEY,
    used_bytes  INTEGER NOT NULL DEFAULT 0,
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    dirs_json   TEXT NOT NULL DEFAULT '{}',
    recorded_at TEXT NOT NULL
);
