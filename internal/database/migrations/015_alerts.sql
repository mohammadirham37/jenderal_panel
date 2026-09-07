CREATE TABLE IF NOT EXISTS alert_rules (
    id          TEXT PRIMARY KEY,
    metric      TEXT NOT NULL,
    operator    TEXT NOT NULL,
    threshold   REAL NOT NULL,
    duration_s  INTEGER DEFAULT 0,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS alert_history (
    id         TEXT PRIMARY KEY,
    rule_id    TEXT REFERENCES alert_rules(id) ON DELETE CASCADE,
    metric     TEXT NOT NULL,
    value      REAL NOT NULL,
    message    TEXT NOT NULL,
    resolved   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_channels (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    config     TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_alert_history_rule_id ON alert_history(rule_id);
CREATE INDEX IF NOT EXISTS idx_alert_history_created_at ON alert_history(created_at);
