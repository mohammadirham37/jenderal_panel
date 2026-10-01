-- Login-as (impersonation): an admin can start a session for another user.
-- The column remembers the admin session so the panel can switch back; it
-- stays empty for ordinary logins.
ALTER TABLE sessions ADD COLUMN impersonator_session_id TEXT NOT NULL DEFAULT '';
