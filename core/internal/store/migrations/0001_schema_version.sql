-- 0001: schema version ledger. Every later migration appends one row here.
CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL,
    applied_at TEXT    NOT NULL -- UTC RFC 3339
);
