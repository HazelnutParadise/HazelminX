-- 0002: job engine (spec 33, ticket 008).
--
-- jobs keeps one row per job. The queryable columns are mirrored from the
-- JSON document in `doc`, which is the full record (steps, resources,
-- diagnostics, completion report). Steps are owned by their job and always
-- read together with it, so they live inside the document rather than in a
-- table of their own. Rows are history: a finished job is never deleted or
-- rewritten, so there is no deleted_at.
CREATE TABLE IF NOT EXISTS jobs (
    id                 TEXT PRIMARY KEY,
    request_id         TEXT    NOT NULL UNIQUE,   -- idempotency key (ENG-002)
    request_hash       TEXT    NOT NULL,          -- sha256 of kind+params+resources
    kind               TEXT    NOT NULL,
    node               TEXT    NOT NULL DEFAULT '',
    requester          TEXT    NOT NULL DEFAULT '',
    state              TEXT    NOT NULL,
    state_reason       TEXT    NOT NULL DEFAULT '',
    error_code         TEXT    NOT NULL DEFAULT '',
    progress           INTEGER NOT NULL DEFAULT 0,
    last_event_seq     INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT    NOT NULL,          -- UTC RFC 3339
    updated_at         TEXT    NOT NULL,
    started_at         TEXT,
    finished_at        TEXT,
    last_heartbeat_at  TEXT,
    doc                TEXT    NOT NULL           -- JSON of the full job
);
CREATE INDEX IF NOT EXISTS jobs_state_created_idx ON jobs (state, created_at);
CREATE INDEX IF NOT EXISTS jobs_kind_created_idx  ON jobs (kind, created_at);

-- job_events is the per-job event log. seq is contiguous per job so a client
-- that reconnects asks for seq > last seen (spec 33.3). Events are append
-- only.
CREATE TABLE IF NOT EXISTS job_events (
    job_id   TEXT    NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
    seq      INTEGER NOT NULL,
    at       TEXT    NOT NULL,                    -- UTC RFC 3339
    type     TEXT    NOT NULL,
    step     INTEGER NOT NULL DEFAULT 0,
    message  TEXT    NOT NULL DEFAULT '',
    data     TEXT,                                -- JSON or NULL
    PRIMARY KEY (job_id, seq)
) WITHOUT ROWID;
