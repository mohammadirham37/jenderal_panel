-- Known login sources per user: lets the panel notify when a successful
-- login comes from an IP never seen before for that account.
CREATE TABLE IF NOT EXISTS known_logins (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id TEXT NOT NULL,
	ip_address TEXT NOT NULL,
	first_seen TEXT NOT NULL,
	last_seen TEXT NOT NULL,
	UNIQUE(user_id, ip_address)
);

CREATE INDEX IF NOT EXISTS idx_known_logins_user_id ON known_logins(user_id);
