-- Per-site WordPress security hardening toggles applied from the WP Toolkit.
CREATE TABLE IF NOT EXISTS website_wp_hardening (
    website_id          TEXT PRIMARY KEY REFERENCES websites(id) ON DELETE CASCADE,
    block_upload_php    INTEGER NOT NULL DEFAULT 0,
    block_includes_php  INTEGER NOT NULL DEFAULT 0,
    block_sensitive     INTEGER NOT NULL DEFAULT 0,
    block_author_scan   INTEGER NOT NULL DEFAULT 0,
    security_headers    INTEGER NOT NULL DEFAULT 0,
    disallow_file_edit  INTEGER NOT NULL DEFAULT 0,
    force_ssl_admin     INTEGER NOT NULL DEFAULT 0,
    disable_debug       INTEGER NOT NULL DEFAULT 0,
    protect_wp_config   INTEGER NOT NULL DEFAULT 0,
    updated_at          TEXT NOT NULL
);
