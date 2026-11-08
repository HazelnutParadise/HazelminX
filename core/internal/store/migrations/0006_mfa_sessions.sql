-- 0006: second factors, recovery codes and session controls (ticket 014;
-- IAM-002, IAM-003, IAM-011, SET-004). Times are UTC RFC 3339 text.
-- Credential rows belong to their user: users are never hard-deleted by the
-- application (they are disabled), so ON DELETE CASCADE only fires on a
-- manual purge, where orphaned factors would be meaningless anyway. The audit
-- ledger keeps the history of every enrolment and removal.

-- One authenticator-app seed per user. The seed itself lives in the secret
-- store (secret_id, ticket 011); this row only says which secret it is,
-- whether enrolment was confirmed with a first code, and the last accepted
-- time step so a code cannot be replayed.
CREATE TABLE IF NOT EXISTS totp_credentials (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    secret_id      TEXT NOT NULL REFERENCES secrets(id) ON DELETE RESTRICT,
    last_used_step INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    confirmed_at   TEXT,          -- NULL while enrolment is pending
    last_used_at   TEXT
);

-- Passkeys (WebAuthn). Public keys are not secret. rp_id is the domain the
-- credential was created for; a management-domain change makes credentials
-- for other domains unusable (IAM-011, T-130). credential_json is the
-- library's credential record (public key, flags, sign counter, transports).
CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id   BLOB NOT NULL UNIQUE,
    rp_id           TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    credential_json TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    last_used_at    TEXT
);
CREATE INDEX IF NOT EXISTS webauthn_credentials_user_idx ON webauthn_credentials(user_id);

-- Single-use recovery codes, stored only as HMAC-SHA256 under a key kept in
-- the secret store. A code is spent by setting used_at; regenerating
-- replaces the whole set.
CREATE TABLE IF NOT EXISTS recovery_codes (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    created_at TEXT NOT NULL,
    used_at    TEXT
);
CREATE INDEX IF NOT EXISTS recovery_codes_unused_idx ON recovery_codes(user_id) WHERE used_at IS NULL;

-- Per-user idle timeout (SET-004). NULL means the platform default.
ALTER TABLE users ADD COLUMN session_idle_seconds INTEGER;

-- How the session was signed in, and when the user last proved it again for
-- a credential change (IAM-011 再次驗證).
ALTER TABLE sessions ADD COLUMN second_factor TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN reauthenticated_at TEXT;
