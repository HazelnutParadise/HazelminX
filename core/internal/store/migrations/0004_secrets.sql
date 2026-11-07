-- 0002: secret references and master-key generations (spec 34.1 機密參照, 35.2; ticket 011).
-- Key material never lives in this database: master_keys records generations only,
-- the bytes stay in the keyring file beside the database (see internal/secrets).

CREATE TABLE IF NOT EXISTS master_keys (
    key_id       TEXT PRIMARY KEY,
    status       TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    created_at   TEXT NOT NULL,          -- UTC RFC 3339
    activated_at TEXT NOT NULL,
    retired_at   TEXT
);

CREATE TABLE IF NOT EXISTS secrets (
    id              TEXT PRIMARY KEY,                     -- opaque "sec_<random>"
    kind            TEXT NOT NULL,                        -- password | private-key | api-token | oauth-refresh | backup-credential | generic
    name            TEXT NOT NULL,
    current_version INTEGER NOT NULL DEFAULT 0,
    scope           TEXT NOT NULL DEFAULT '[]',           -- JSON array of consumer refs allowed to reveal
    expires_at      TEXT,                                 -- UTC RFC 3339, NULL = never
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    deleted_at      TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS secrets_name_active_idx ON secrets(name) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS secret_versions (
    secret_id  TEXT    NOT NULL REFERENCES secrets(id) ON DELETE RESTRICT,
    version    INTEGER NOT NULL,
    key_id     TEXT    NOT NULL REFERENCES master_keys(key_id) ON DELETE RESTRICT,
    ciphertext BLOB    NOT NULL,                          -- AES-256-GCM nonce || ciphertext || tag, AAD = secret_id "@" version
    created_at TEXT    NOT NULL,
    PRIMARY KEY (secret_id, version)
);
CREATE INDEX IF NOT EXISTS secret_versions_key_idx ON secret_versions(key_id);

-- Change record for secrets. Never holds a value, old or new (DAT-004).
CREATE TABLE IF NOT EXISTS secret_changes (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    secret_id    TEXT    NOT NULL,                        -- no FK: the record outlives the secret
    secret_name  TEXT    NOT NULL,                        -- snapshot at the time of the change
    action       TEXT    NOT NULL,                        -- created | rotated | scope-changed | expiry-changed | deleted | rewrapped
    from_version INTEGER,
    to_version   INTEGER,
    key_id       TEXT,                                    -- master key generation after the change
    actor        TEXT    NOT NULL,
    occurred_at  TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS secret_changes_secret_idx ON secret_changes(secret_id, occurred_at);
