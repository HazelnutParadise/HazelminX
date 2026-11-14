-- 0013: the container engines each node's host has (ticket 039.1, NOD-003,
-- NOD-004, ENV-001, ENV-002, INS-008). One row per node: the latest report
-- the privileged executor produced, and the latest failure to get one.
-- report is the executor's JSON (protocol.EngineReport); a value the host
-- did not provide is null inside it. Replaced on every probe; nothing is
-- kept from older reports.
CREATE TABLE IF NOT EXISTS host_engines (
    node_id       TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE RESTRICT,
    -- NULL until a report arrived.
    report        TEXT,
    collected_at  TEXT,
    received_at   TEXT,
    -- The last probe that failed, cleared by the next report. error_code is
    -- an engines_unavailable code or one of the engines package's codes.
    error_code    TEXT,
    error_message TEXT,
    error_at      TEXT,
    CHECK ((report IS NULL) = (collected_at IS NULL) AND (report IS NULL) = (received_at IS NULL)),
    CHECK ((error_code IS NULL) = (error_at IS NULL))
);
