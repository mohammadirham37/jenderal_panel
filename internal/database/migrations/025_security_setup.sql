CREATE TABLE IF NOT EXISTS security_setup_runs (
    id TEXT PRIMARY KEY,
    request_json TEXT NOT NULL,
    review_json TEXT NOT NULL,
    review_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending','running','failed','completed')),
    safe_error TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS security_setup_steps (
    run_id TEXT NOT NULL,
    step_name TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('completed','failed')),
    safe_error TEXT NOT NULL DEFAULT '',
    completed_at TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(run_id, step_name),
    FOREIGN KEY(run_id) REFERENCES security_setup_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_security_setup_runs_updated ON security_setup_runs(updated_at DESC);
