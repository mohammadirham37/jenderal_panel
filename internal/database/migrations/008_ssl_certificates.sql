CREATE TABLE IF NOT EXISTS ssl_certificates (
    id            TEXT PRIMARY KEY,
    website_id    TEXT NOT NULL REFERENCES websites(id) ON DELETE CASCADE,
    domain        TEXT NOT NULL,
    issuer        TEXT NOT NULL DEFAULT 'letsencrypt',
    status        TEXT NOT NULL DEFAULT 'pending',
    expires_at    TEXT,
    auto_renew    INTEGER NOT NULL DEFAULT 1,
    error_message TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ssl_certificates_website_id ON ssl_certificates(website_id);
CREATE INDEX IF NOT EXISTS idx_ssl_certificates_expires_at ON ssl_certificates(expires_at);
