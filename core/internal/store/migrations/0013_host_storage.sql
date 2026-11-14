-- 0013: storage inventory, usage and capacity alert rules (ticket 030.1,
-- STO-001, STO-010). Keyed by node id. A missing value is NULL ("not
-- provided"), never 0.

-- The latest storage inventory each node reported (agent JSON: devices,
-- mounts, mount configuration, swap areas, problems).
CREATE TABLE IF NOT EXISTS host_storage (
    node_id      TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE RESTRICT,
    inventory    TEXT NOT NULL,
    collected_at TEXT NOT NULL,
    received_at  TEXT NOT NULL
);

-- The latest finished usage measurement of each node (agent JSON:
-- per-filesystem bytes by category).
CREATE TABLE IF NOT EXISTS host_storage_usage (
    node_id     TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE RESTRICT,
    usage       TEXT NOT NULL,
    started_at  TEXT NOT NULL,
    received_at TEXT NOT NULL
);

-- Capacity alert thresholds. node_id '' applies to every node and
-- mount_point '' to every filesystem; the most specific rule wins
-- (node and mount, then node, then mount, then the default row). A NULL
-- threshold is off for that rule. Percentages are of the filesystem
-- size (bytes) or inode count.
CREATE TABLE IF NOT EXISTS storage_alert_rules (
    node_id                    TEXT NOT NULL,
    mount_point                TEXT NOT NULL,
    warn_available_percent     REAL CHECK (warn_available_percent BETWEEN 0 AND 100),
    critical_available_percent REAL CHECK (critical_available_percent BETWEEN 0 AND 100),
    critical_available_bytes   INTEGER CHECK (critical_available_bytes >= 0),
    warn_inodes_free_percent   REAL CHECK (warn_inodes_free_percent BETWEEN 0 AND 100),
    critical_inodes_free_percent REAL CHECK (critical_inodes_free_percent BETWEEN 0 AND 100),
    warn_full_within_days      INTEGER CHECK (warn_full_within_days > 0),
    updated_at                 TEXT NOT NULL,
    updated_by                 TEXT NOT NULL,
    PRIMARY KEY (node_id, mount_point)
) WITHOUT ROWID;
