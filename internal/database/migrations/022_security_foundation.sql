CREATE TABLE IF NOT EXISTS security_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO security_settings (key, value, updated_at)
VALUES ('security.event_retention_days', '90', CURRENT_TIMESTAMP);

CREATE TABLE IF NOT EXISTS security_events (
    id                 TEXT PRIMARY KEY,
    fingerprint        TEXT NOT NULL,
    category           TEXT NOT NULL,
    severity           TEXT NOT NULL CHECK(severity IN ('info','low','medium','high','critical')),
    component          TEXT NOT NULL,
    resource           TEXT NOT NULL DEFAULT '',
    evidence           TEXT NOT NULL DEFAULT '',
    recommended_action TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL CHECK(status IN ('open','acknowledged','resolved','false_positive')),
    occurrence_count   INTEGER NOT NULL DEFAULT 1 CHECK(occurrence_count > 0),
    first_seen         TEXT NOT NULL,
    last_seen          TEXT NOT NULL,
    notified_at        TEXT,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_security_events_status_last_seen
    ON security_events(status, last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_security_events_component_last_seen
    ON security_events(component, last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_security_events_fingerprint
    ON security_events(fingerprint, last_seen DESC);

CREATE TABLE IF NOT EXISTS security_event_occurrences (
    id          TEXT PRIMARY KEY,
    event_id    TEXT NOT NULL REFERENCES security_events(id) ON DELETE CASCADE,
    evidence    TEXT NOT NULL DEFAULT '',
    observed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_security_event_occurrences_event
    ON security_event_occurrences(event_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS security_manual_bans (
    id               TEXT PRIMARY KEY,
    component        TEXT NOT NULL,
    jail             TEXT NOT NULL,
    address          TEXT NOT NULL,
    requested_expiry TEXT NOT NULL,
    actual_expiry    TEXT,
    status           TEXT NOT NULL CHECK(status IN ('active','released')),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_security_manual_bans_active_expiry
    ON security_manual_bans(status, requested_expiry);
