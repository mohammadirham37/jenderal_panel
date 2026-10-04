-- Disk-saving mode: after a successful off-site upload the local file is
-- removed and the backup is marked remote-only (the remote copy is the only
-- remaining one; download/restore/delete must go through the remote backend).
ALTER TABLE backups ADD COLUMN remote_only INTEGER NOT NULL DEFAULT 0;
