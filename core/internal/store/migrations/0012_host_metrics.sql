-- 0012: host facts and resource history (ticket 020.1, SYS-001, MON-001,
-- MON-005, ROB-009). Keyed by node id, so history continues when a node
-- re-enrolls with a new key. A missing value is NULL ("not provided"),
-- never 0. Samples are telemetry: rows past their retention are deleted
-- (raw points 48 hours, 5-minute rollups 90 days); nothing else deletes.

-- The latest facts each node reported. facts is the agent's JSON (every
-- field {value, source}).
CREATE TABLE IF NOT EXISTS host_facts (
    node_id      TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE RESTRICT,
    facts        TEXT NOT NULL,
    collected_at TEXT NOT NULL,
    received_at  TEXT NOT NULL
);

-- The latest full sample of each node (every filesystem, disk, interface
-- and sensor), and counters about the stream.
CREATE TABLE IF NOT EXISTS host_metric_latest (
    node_id          TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE RESTRICT,
    sample           TEXT NOT NULL,
    collected_at     TEXT NOT NULL,
    received_at      TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds > 0),
    -- Samples the agent reported dropping (its buffer was full while Core
    -- was unreachable), in total.
    dropped_samples  INTEGER NOT NULL DEFAULT 0 CHECK (dropped_samples >= 0)
);

-- One value of one series at one sample. series names the metric
-- ("cpu.usage", "disk.readLatencyMs"); key names the thing it belongs to
-- ("" for host-wide series, a disk or interface name, a mount point, a
-- core number, "chip/label" for a sensor). at is RFC 3339 UTC with whole
-- seconds, so text order is time order. interval_seconds is the agent's
-- sampling interval, the resolution of the point.
CREATE TABLE IF NOT EXISTS host_metric_points (
    node_id          TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    series           TEXT NOT NULL,
    series_key       TEXT NOT NULL,
    at               TEXT NOT NULL,
    value            REAL NOT NULL,
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds > 0),
    PRIMARY KEY (node_id, series, series_key, at)
) WITHOUT ROWID;

-- Retention deletes by time across every series.
CREATE INDEX IF NOT EXISTS host_metric_points_at ON host_metric_points (at);

-- 5-minute rollups: average (value_sum / samples), minimum and maximum of
-- the points in [bucket_start, bucket_start + 300 s).
CREATE TABLE IF NOT EXISTS host_metric_rollups (
    node_id      TEXT NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    series       TEXT NOT NULL,
    series_key   TEXT NOT NULL,
    bucket_start TEXT NOT NULL,
    value_sum    REAL NOT NULL,
    value_min    REAL NOT NULL,
    value_max    REAL NOT NULL,
    samples      INTEGER NOT NULL CHECK (samples > 0),
    PRIMARY KEY (node_id, series, series_key, bucket_start)
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS host_metric_rollups_bucket ON host_metric_rollups (bucket_start);
