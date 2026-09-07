CREATE TABLE IF NOT EXISTS deployments (
    id          TEXT PRIMARY KEY,
    website_id  TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    commit_hash TEXT,
    branch      TEXT NOT NULL DEFAULT 'main',
    status      TEXT NOT NULL DEFAULT 'pending',
    duration_ms INTEGER,
    log         TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_deployments_website_id ON deployments(website_id);
