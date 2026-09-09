ALTER TABLE websites ADD COLUMN framework TEXT NOT NULL DEFAULT 'none';
ALTER TABLE websites ADD COLUMN framework_version TEXT NOT NULL DEFAULT '';
ALTER TABLE websites ADD COLUMN frontend_stack TEXT NOT NULL DEFAULT '';
ALTER TABLE websites ADD COLUMN inertia_adapter TEXT NOT NULL DEFAULT '';
ALTER TABLE websites ADD COLUMN project_variant TEXT NOT NULL DEFAULT 'empty';
ALTER TABLE websites ADD COLUMN setup_mode TEXT NOT NULL DEFAULT 'config-only';
ALTER TABLE websites ADD COLUMN provision_stage TEXT NOT NULL DEFAULT '';
ALTER TABLE websites ADD COLUMN provision_log TEXT NOT NULL DEFAULT '';
