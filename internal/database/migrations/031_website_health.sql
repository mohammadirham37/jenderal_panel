CREATE TABLE IF NOT EXISTS website_health_checks (
    website_id TEXT PRIMARY KEY,
    url TEXT NOT NULL DEFAULT '',
    expected_status INTEGER NOT NULL DEFAULT 200,
    enabled INTEGER NOT NULL DEFAULT 0,
    last_status INTEGER,
    last_latency_ms INTEGER,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    last_checked_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);
