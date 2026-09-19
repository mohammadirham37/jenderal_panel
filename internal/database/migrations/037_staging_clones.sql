-- Staging clones: tracks website → staging-site copy operations so the UI
-- can show their progress (waiting for provisioning, copying, done/failed).
CREATE TABLE IF NOT EXISTS staging_clones (
	id                TEXT PRIMARY KEY,
	source_website_id TEXT NOT NULL,
	target_website_id TEXT NOT NULL,
	target_domain     TEXT NOT NULL,
	status            TEXT NOT NULL,
	task_id           TEXT NOT NULL DEFAULT '',
	error             TEXT NOT NULL DEFAULT '',
	created_at        TEXT NOT NULL,
	updated_at        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_staging_clones_source ON staging_clones(source_website_id);
