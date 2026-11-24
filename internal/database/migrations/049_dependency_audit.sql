-- Dependency CVE audit (internal/depaudit): the latest scan of the
-- websites' Composer/npm lockfiles against OSV.dev.
CREATE TABLE IF NOT EXISTS dependency_scans (
    id          TEXT PRIMARY KEY,
    trigger     TEXT NOT NULL,
    status      TEXT NOT NULL,
    error       TEXT NOT NULL DEFAULT '',
    sites       INTEGER NOT NULL DEFAULT 0,
    packages    INTEGER NOT NULL DEFAULT 0,
    findings    INTEGER NOT NULL DEFAULT 0,
    started_at  TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_dependency_scans_started ON dependency_scans(started_at);

CREATE TABLE IF NOT EXISTS dependency_findings (
    scan_id       TEXT NOT NULL REFERENCES dependency_scans(id) ON DELETE CASCADE,
    website_id    TEXT NOT NULL,
    domain        TEXT NOT NULL,
    ecosystem     TEXT NOT NULL,
    package       TEXT NOT NULL,
    version       TEXT NOT NULL,
    dev           INTEGER NOT NULL DEFAULT 0,
    lockfile      TEXT NOT NULL,
    vuln_id       TEXT NOT NULL,
    aliases       TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    fixed_version TEXT NOT NULL DEFAULT '',
    url           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_dependency_findings_scan ON dependency_findings(scan_id);

CREATE TABLE IF NOT EXISTS osv_vulns (
    id         TEXT PRIMARY KEY,
    modified   TEXT NOT NULL,
    data       TEXT NOT NULL,
    fetched_at TEXT NOT NULL
);
