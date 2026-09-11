ALTER TABLE users ADD COLUMN ssh_enabled INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS user_ssh_keys (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    name        TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    algo        TEXT NOT NULL,
    bits        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_user_ssh_keys_user ON user_ssh_keys(user_id);
