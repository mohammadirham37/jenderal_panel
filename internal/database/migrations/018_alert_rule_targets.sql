CREATE TABLE IF NOT EXISTS alert_rule_targets (
    rule_id TEXT PRIMARY KEY REFERENCES alert_rules(id) ON DELETE CASCADE,
    target  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_alert_history_rule_resolved
ON alert_history(rule_id, resolved, created_at DESC);
