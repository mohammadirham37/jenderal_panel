CREATE TABLE IF NOT EXISTS traffic_guard_profiles (
    website_id TEXT PRIMARY KEY, mode TEXT NOT NULL DEFAULT 'observe' CHECK(mode IN ('observe','balanced','strict','custom')),
    proxy_mode TEXT NOT NULL DEFAULT 'direct' CHECK(proxy_mode IN ('direct','cloudflare','custom')),
    proxy_header TEXT NOT NULL DEFAULT '', proxy_cidrs TEXT NOT NULL DEFAULT '[]', requests_per_second INTEGER NOT NULL DEFAULT 10,
    burst INTEGER NOT NULL DEFAULT 20, connections INTEGER NOT NULL DEFAULT 20, observe_started_at TEXT NOT NULL,
    created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS traffic_log_cursors (website_id TEXT PRIMARY KEY, inode INTEGER NOT NULL, offset INTEGER NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS traffic_minute_buckets (
    website_id TEXT NOT NULL, bucket_at TEXT NOT NULL, requests INTEGER NOT NULL, status_4xx INTEGER NOT NULL,
    status_5xx INTEGER NOT NULL, status_429 INTEGER NOT NULL, bytes INTEGER NOT NULL, peak_rps INTEGER NOT NULL,
    top_ips TEXT NOT NULL DEFAULT '{}', top_paths TEXT NOT NULL DEFAULT '{}', top_agents TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY(website_id,bucket_at)
);
CREATE TABLE IF NOT EXISTS traffic_hour_buckets (
    website_id TEXT NOT NULL, bucket_at TEXT NOT NULL, requests INTEGER NOT NULL, status_4xx INTEGER NOT NULL,
    status_5xx INTEGER NOT NULL, status_429 INTEGER NOT NULL, bytes INTEGER NOT NULL, peak_rps INTEGER NOT NULL,
    PRIMARY KEY(website_id,bucket_at)
);
CREATE TABLE IF NOT EXISTS traffic_baselines (
    website_id TEXT PRIMARY KEY, sample_count INTEGER NOT NULL DEFAULT 0, mean_rpm REAL NOT NULL DEFAULT 0,
    m2_rpm REAL NOT NULL DEFAULT 0, first_sample_at TEXT, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS trusted_proxy_snapshots (
    id INTEGER PRIMARY KEY CHECK(id=1), ipv4_cidrs TEXT NOT NULL, ipv6_cidrs TEXT NOT NULL,
    fetched_at TEXT NOT NULL, consecutive_failures INTEGER NOT NULL DEFAULT 0
);
