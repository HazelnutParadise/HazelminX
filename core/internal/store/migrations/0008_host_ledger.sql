-- 0008: Core's copy of each host's change ledger (spec 55.1, UNX-001,
-- ticket 113). The host keeps the original (root-only, hash-chained); Core
-- stores every line byte for byte after checking the chain, so the copy can
-- be verified the same way and survives the host being unreachable.
-- node_id is the node fingerprint; the node registry (ticket 019) does not
-- exist yet, so there is no foreign key to it.

-- One row per node: which host ledger the copy follows and where it ends.
-- ledger_id is '' until the first page is stored. The last_error_* columns
-- say why the latest sync attempt failed (cleared by the next success).
CREATE TABLE IF NOT EXISTS host_ledgers (
    node_id         TEXT PRIMARY KEY,
    ledger_id       TEXT NOT NULL DEFAULT '',
    head_seq        INTEGER NOT NULL DEFAULT 0 CHECK (head_seq >= 0),
    head_hash       TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    last_synced_at  TEXT,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_error      TEXT NOT NULL DEFAULT '',
    last_error_at   TEXT
);

-- Append-only, like the host file. line is the raw ledger line; hash is the
-- SHA-256 of its entry bytes. Keyed by ledger_id too, so a host whose ledger
-- was recreated never overwrites the lines Core already holds.
CREATE TABLE IF NOT EXISTS host_ledger_lines (
    node_id     TEXT NOT NULL REFERENCES host_ledgers(node_id) ON DELETE RESTRICT,
    ledger_id   TEXT NOT NULL,
    seq         INTEGER NOT NULL CHECK (seq >= 1),
    hash        TEXT NOT NULL,
    line        TEXT NOT NULL,
    received_at TEXT NOT NULL,
    PRIMARY KEY (node_id, ledger_id, seq)
);

CREATE TRIGGER IF NOT EXISTS host_ledger_lines_no_update
BEFORE UPDATE ON host_ledger_lines
BEGIN
    SELECT RAISE(ABORT, 'host ledger lines are append-only');
END;

CREATE TRIGGER IF NOT EXISTS host_ledger_lines_no_delete
BEFORE DELETE ON host_ledger_lines
BEGIN
    SELECT RAISE(ABORT, 'host ledger lines are append-only');
END;
