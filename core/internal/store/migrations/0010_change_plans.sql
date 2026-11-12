-- 0010: change plans and their approval (ticket 016.1, IAM-005, ENG-005).
--
-- A plan is the reviewed content of one high-risk change: its targets with
-- the versions they had, the diff, the affected services, the prechecks and
-- the approval it needs. `digest` is the SHA-256 of that content; an
-- approval is bound to it and to expires_at, and the plan is computed again
-- before approval and before execution, so a changed content or a moved
-- resource version invalidates the approval instead of running something
-- nobody reviewed. Plans are history: they are never deleted or rewritten
-- after they end, so there is no deleted_at.
CREATE TABLE IF NOT EXISTS change_plans (
    id               TEXT PRIMARY KEY,
    kind             TEXT    NOT NULL,
    state            TEXT    NOT NULL CHECK (state IN ('awaiting_approval', 'approved', 'rejected', 'withdrawn', 'expired', 'invalidated', 'executed')),
    digest           TEXT    NOT NULL,
    -- Random per plan; mixed into the digest of secret values so the
    -- published digest cannot be used to guess a short secret.
    secret_salt      BLOB    NOT NULL,
    requested_by     TEXT    NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at       TEXT    NOT NULL,          -- UTC RFC 3339
    expires_at       TEXT    NOT NULL,
    updated_at       TEXT    NOT NULL,
    approved_by      TEXT REFERENCES users(id) ON DELETE RESTRICT,
    approved_at      TEXT,
    approved_digest  TEXT,
    auto_approved    INTEGER NOT NULL DEFAULT 0 CHECK (auto_approved IN (0, 1)),
    -- Who ended the plan without running it (rejected, withdrawn,
    -- invalidated) and why.
    decided_by       TEXT REFERENCES users(id) ON DELETE RESTRICT,
    decided_at       TEXT,
    decision_reason  TEXT    NOT NULL DEFAULT '',
    -- The job request id is reserved before the job is submitted, so a
    -- replayed execute finds the same job.
    request_id       TEXT UNIQUE,
    job_id           TEXT,
    executed_by      TEXT REFERENCES users(id) ON DELETE RESTRICT,
    executed_at      TEXT,
    doc              TEXT    NOT NULL           -- JSON of the plan content
);
CREATE INDEX IF NOT EXISTS change_plans_state_expires_idx ON change_plans (state, expires_at);
CREATE INDEX IF NOT EXISTS change_plans_created_idx       ON change_plans (created_at, id);

-- One row per target scope of a plan. A plan is visible only to accounts
-- that can see every one of its targets, and lists filter on this table.
CREATE TABLE IF NOT EXISTS change_plan_scopes (
    plan_id    TEXT NOT NULL REFERENCES change_plans(id) ON DELETE RESTRICT,
    scope_kind TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    PRIMARY KEY (plan_id, scope_kind, scope_id)
);
