-- A database user can be granted full access or read-only access to a
-- managed database. Existing grants were all full access.
ALTER TABLE db_grants ADD COLUMN access TEXT NOT NULL DEFAULT 'full';
