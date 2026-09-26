-- Tracks which database users have been granted privileges on which managed
-- databases. The panel never stored grants before, so access could not be
-- shown or revoked.
CREATE TABLE IF NOT EXISTS db_grants (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    database_id TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    UNIQUE(user_id, database_id)
);

CREATE INDEX IF NOT EXISTS idx_db_grants_user ON db_grants(user_id);
CREATE INDEX IF NOT EXISTS idx_db_grants_database ON db_grants(database_id);
