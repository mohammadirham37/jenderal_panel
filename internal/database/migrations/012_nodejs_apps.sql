CREATE TABLE IF NOT EXISTS nodejs_apps (
    id           TEXT PRIMARY KEY,
    website_id   TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    node_version TEXT NOT NULL,
    package_mgr  TEXT NOT NULL DEFAULT 'npm',
    build_cmd    TEXT,
    start_cmd    TEXT NOT NULL,
    port         INTEGER NOT NULL,
    env_vars     TEXT,
    status       TEXT NOT NULL DEFAULT 'stopped',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_nodejs_apps_website_id ON nodejs_apps(website_id);
