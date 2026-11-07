-- 0002: IAM foundation (ticket 009, spec chapter 5): local accounts, role
-- assignments with resource scopes, login sessions and the audit ledger.
-- Times are UTC RFC 3339 text. Password hashes are argon2id; session tokens
-- are stored only as SHA-256 so a database read cannot replay a login.

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    display_name  TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

-- One row per (user, role, scope). scope_kind 'global' with scope_id '*'
-- covers every resource; otherwise the pair names one node/project/database.
CREATE TABLE IF NOT EXISTS role_assignments (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    scope_kind TEXT NOT NULL,
    scope_id   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    created_by TEXT,
    UNIQUE (user_id, role, scope_kind, scope_id)
);
CREATE INDEX IF NOT EXISTS role_assignments_user_idx ON role_assignments(user_id);

CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    csrf_token   TEXT NOT NULL,
    source_ip    TEXT NOT NULL,
    user_agent   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    revoked_at   TEXT
);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions(user_id);

-- Append-only. seq orders events and is the pagination cursor; before/after
-- are JSON summaries with secret-looking keys already masked on write.
CREATE TABLE IF NOT EXISTS audit_events (
    seq           INTEGER PRIMARY KEY AUTOINCREMENT,
    id            TEXT NOT NULL UNIQUE,
    at            TEXT NOT NULL,
    actor_id      TEXT,
    actor_name    TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    scope_kind    TEXT NOT NULL,
    scope_id      TEXT NOT NULL,
    resource_kind TEXT NOT NULL DEFAULT '',
    resource_id   TEXT NOT NULL DEFAULT '',
    job_id        TEXT NOT NULL DEFAULT '',
    node_id       TEXT NOT NULL DEFAULT '',
    result        TEXT NOT NULL CHECK (result IN ('ok', 'denied', 'failed')),
    message       TEXT NOT NULL DEFAULT '',
    before_json   TEXT,
    after_json    TEXT,
    request_id    TEXT NOT NULL DEFAULT '',
    source_ip     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS audit_events_scope_idx ON audit_events(scope_kind, scope_id, seq);
CREATE INDEX IF NOT EXISTS audit_events_actor_idx ON audit_events(actor_id, seq);
CREATE INDEX IF NOT EXISTS audit_events_job_idx ON audit_events(job_id) WHERE job_id <> '';
