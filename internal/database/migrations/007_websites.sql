CREATE TABLE IF NOT EXISTS websites (
    id            TEXT PRIMARY KEY,
    domain        TEXT NOT NULL UNIQUE,
    app_type      TEXT NOT NULL DEFAULT 'php',
    php_version   TEXT,
    document_root TEXT NOT NULL,
    web_user      TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    ssl_enabled   INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS domains (
    id         TEXT PRIMARY KEY,
    website_id TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    name       TEXT NOT NULL UNIQUE,
    type       TEXT NOT NULL DEFAULT 'alias',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_domains_website_id ON domains(website_id);
CREATE INDEX IF NOT EXISTS idx_websites_status ON websites(status);
