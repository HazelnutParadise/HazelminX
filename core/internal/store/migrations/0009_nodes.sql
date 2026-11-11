-- 0009: node registry and enrollment (ticket 019, NOD-001, NOD-005..009,
-- SEC-001..003). A node's identity is its id; the key fingerprint is what
-- the agent channel proves and may change when the node re-enrolls with a
-- new key (the old fingerprint then moves to node_retired_keys). IP
-- addresses and host names are information, never identity.

CREATE TABLE IF NOT EXISTS nodes (
    id                     TEXT PRIMARY KEY,
    name                   TEXT NOT NULL,
    tags                   TEXT NOT NULL DEFAULT '[]',
    node_group             TEXT NOT NULL DEFAULT '',
    fingerprint            TEXT NOT NULL UNIQUE,
    public_key             BLOB NOT NULL,
    -- What the host reported at enrollment, shown for verification.
    hostname               TEXT NOT NULL DEFAULT '',
    machine_id             TEXT NOT NULL DEFAULT '',
    os                     TEXT NOT NULL DEFAULT '',
    arch                   TEXT NOT NULL DEFAULT '',
    -- What the agent declared on its latest session.
    agent_version          TEXT NOT NULL DEFAULT '',
    agent_protocol_min     INTEGER NOT NULL DEFAULT 0,
    agent_protocol_max     INTEGER NOT NULL DEFAULT 0,
    protocol_version       INTEGER NOT NULL DEFAULT 0,
    capabilities           TEXT NOT NULL DEFAULT '[]',
    state                  TEXT NOT NULL CHECK (state IN ('active', 'revoked', 'identity_conflict', 'removed')),
    -- The certificate the node uses, and during rotation the one it is
    -- switching to; both are accepted until the node connects with the new one.
    cert_serial            TEXT NOT NULL,
    cert_not_after         TEXT NOT NULL,
    next_cert_serial       TEXT,
    next_cert_not_after    TEXT,
    next_cert_pem          TEXT,
    rotation_requested_at  TEXT,
    rotation_delivered_at  TEXT,
    rotation_error         TEXT NOT NULL DEFAULT '',
    maintenance            INTEGER NOT NULL DEFAULT 0 CHECK (maintenance IN (0, 1)),
    maintenance_reason     TEXT NOT NULL DEFAULT '',
    maintenance_since      TEXT,
    maintenance_ended_at   TEXT,
    recheck_requested_at   TEXT,
    revoked_at             TEXT,
    revoke_reason          TEXT NOT NULL DEFAULT '',
    revoked_by             TEXT NOT NULL DEFAULT '',
    -- When the live session was closed by the revocation, and when the agent
    -- was last told (closed session or refused reconnect); the difference to
    -- revoked_at is the propagation delay (SEC-003).
    revocation_closed_at   TEXT,
    revocation_notified_at TEXT,
    conflict_at            TEXT,
    removed_at             TEXT,
    removed_by             TEXT NOT NULL DEFAULT '',
    removal_mode           TEXT NOT NULL DEFAULT '',
    last_contact_at        TEXT,
    last_connected_at      TEXT,
    last_remote_addr       TEXT NOT NULL DEFAULT '',
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL,
    version                INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1)
);

CREATE INDEX IF NOT EXISTS nodes_machine_id ON nodes (machine_id) WHERE machine_id <> '';

-- Keys a node no longer uses (replaced at re-enrollment). They are refused
-- for good.
CREATE TABLE IF NOT EXISTS node_retired_keys (
    fingerprint TEXT PRIMARY KEY,
    node_id     TEXT NOT NULL REFERENCES nodes (id) ON DELETE RESTRICT,
    retired_at  TEXT NOT NULL,
    reason      TEXT NOT NULL
);

-- One row per enrollment code. A code is single-use, so the claim made with
-- it (the host that used it and the decision on it) lives on the same row.
-- code_key is SHA-256("hazelminx-enroll-v1\n" + code); the code itself is
-- never stored.
CREATE TABLE IF NOT EXISTS node_enrollments (
    id                TEXT PRIMARY KEY,
    code_id           TEXT NOT NULL,
    code_key          BLOB NOT NULL,
    name              TEXT NOT NULL DEFAULT '',
    tags              TEXT NOT NULL DEFAULT '[]',
    node_group        TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL,
    created_at        TEXT NOT NULL,
    expires_at        TEXT NOT NULL,
    revoked_at        TEXT,
    revoked_by        TEXT NOT NULL DEFAULT '',
    claimed_at        TEXT,
    claim_expires_at  TEXT,
    fingerprint       TEXT,
    public_key        BLOB,
    hostname          TEXT NOT NULL DEFAULT '',
    machine_id        TEXT NOT NULL DEFAULT '',
    os                TEXT NOT NULL DEFAULT '',
    arch              TEXT NOT NULL DEFAULT '',
    agent_version     TEXT NOT NULL DEFAULT '',
    source_addr       TEXT NOT NULL DEFAULT '',
    decision          TEXT CHECK (decision IN ('approved', 'rejected')),
    decided_at        TEXT,
    decided_by        TEXT NOT NULL DEFAULT '',
    node_id           TEXT REFERENCES nodes (id) ON DELETE RESTRICT,
    node_certificate  TEXT,
    version           INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1)
);

CREATE INDEX IF NOT EXISTS node_enrollments_code_id ON node_enrollments (code_id);
