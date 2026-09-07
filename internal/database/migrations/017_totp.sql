-- TOTP support: add columns to users table
-- SQLite ignores ALTER ADD COLUMN if column already exists (error is caught by migration runner)
-- Using a separate table approach for compatibility

CREATE TABLE IF NOT EXISTS user_totp (
    user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret      TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);
