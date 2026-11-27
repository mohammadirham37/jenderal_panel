-- The repository each deployment ran from, shown in the deployment history.
-- Earlier rows only kept it transiently in the log column, so they stay empty.
ALTER TABLE deployments ADD COLUMN repo TEXT NOT NULL DEFAULT '';
