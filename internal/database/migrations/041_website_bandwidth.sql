-- Cumulative per-website bandwidth, one row per website per month (UTC).
-- Fed by the traffic log collector so usage survives traffic bucket
-- retention (minute buckets are pruned after a day, hourly after 30 days).
CREATE TABLE IF NOT EXISTS website_bandwidth_monthly (
    website_id TEXT NOT NULL,
    month      TEXT NOT NULL,             -- 'YYYY-MM' (UTC)
    bytes      INTEGER NOT NULL DEFAULT 0,
    requests   INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (website_id, month)
);
