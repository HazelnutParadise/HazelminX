-- 0006: management API contract (ticket 010, spec 36, addendum 03).
-- Times are UTC RFC 3339 text.

-- Versioned writes (36.1, 34.2): the version grows by one on every change
-- to the user's representation, role assignments included, and is sent as
-- the ETag. Existing rows start at 1.
ALTER TABLE users ADD COLUMN version INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1);

-- ROB-010 confirmations. A change that may cut the management path is
-- applied, then waits for its operator to confirm from a connection opened
-- after applied_at. Rows are history: they move through states and are not
-- deleted, so there is no deleted_at. The countdown, automatic revert and
-- restart handling belong to ticket 024.
CREATE TABLE IF NOT EXISTS confirmations (
    id                 TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    summary            TEXT NOT NULL DEFAULT '',
    resources          TEXT NOT NULL,              -- JSON array of job resource refs
    job_id             TEXT NOT NULL DEFAULT '',   -- job that applied the change, if any
    require_host       TEXT NOT NULL DEFAULT '',   -- new entry host (domain change)
    require_local_addr TEXT NOT NULL DEFAULT '',   -- new listen address (address or port change)
    state              TEXT NOT NULL CHECK (state IN ('pending', 'confirmed', 'expired', 'rolled_back')),
    applied_at         TEXT NOT NULL,
    expires_at         TEXT NOT NULL,
    confirmed_at       TEXT,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS confirmations_state_idx ON confirmations (state, expires_at);
CREATE INDEX IF NOT EXISTS confirmations_user_idx ON confirmations (user_id);

-- Background list queries (ROB-008 "改成背景工作"). One row per query job;
-- items are kept apart from the job document so a large result never slows
-- the job list. Items are purged after expires_at (7 days); the query row
-- stays as history with purged_at set.
CREATE TABLE IF NOT EXISTS list_queries (
    job_id       TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE RESTRICT,
    requester_id TEXT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    list         TEXT NOT NULL,
    query        TEXT NOT NULL,                    -- JSON {sort, filters}
    scope_key    TEXT NOT NULL DEFAULT '',         -- what the requester could see when the rows were stored
    total        INTEGER,                          -- rows matched; NULL until finished
    stored       INTEGER NOT NULL DEFAULT 0,       -- rows kept (at most the result cap)
    truncated    INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    purged_at    TEXT
);
CREATE INDEX IF NOT EXISTS list_queries_expiry_idx ON list_queries (expires_at) WHERE purged_at IS NULL;
CREATE INDEX IF NOT EXISTS list_queries_requester_idx ON list_queries (requester_id) WHERE purged_at IS NULL;

CREATE TABLE IF NOT EXISTS list_query_items (
    job_id TEXT    NOT NULL REFERENCES list_queries(job_id) ON DELETE CASCADE,
    pos    INTEGER NOT NULL CHECK (pos >= 0),
    item   TEXT    NOT NULL,                       -- JSON of one list item
    PRIMARY KEY (job_id, pos)
) WITHOUT ROWID;
