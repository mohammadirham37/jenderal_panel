-- Git deployment settings per website
-- Uses ALTER TABLE which is idempotent-safe via schema_migrations tracking

ALTER TABLE websites ADD COLUMN git_repo TEXT DEFAULT '';
ALTER TABLE websites ADD COLUMN git_branch TEXT DEFAULT 'main';
ALTER TABLE websites ADD COLUMN git_provider TEXT DEFAULT '';
