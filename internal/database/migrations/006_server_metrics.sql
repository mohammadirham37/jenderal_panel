CREATE TABLE IF NOT EXISTS server_metrics (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    cpu        REAL NOT NULL,
    ram_used   INTEGER NOT NULL,
    ram_total  INTEGER NOT NULL,
    swap_used  INTEGER NOT NULL,
    swap_total INTEGER NOT NULL,
    disk_used  INTEGER NOT NULL,
    disk_total INTEGER NOT NULL,
    load_1     REAL NOT NULL,
    load_5     REAL NOT NULL,
    load_15    REAL NOT NULL,
    net_rx     INTEGER NOT NULL,
    net_tx     INTEGER NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_server_metrics_created_at ON server_metrics(created_at);
