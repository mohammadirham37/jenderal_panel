CREATE TABLE IF NOT EXISTS cron_jobs (
    id          TEXT PRIMARY KEY,
    website_id  TEXT REFERENCES websites(id) ON DELETE CASCADE,
    command     TEXT NOT NULL,
    schedule    TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    last_run    TEXT,
    last_status TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cron_jobs_website_id ON cron_jobs(website_id);
