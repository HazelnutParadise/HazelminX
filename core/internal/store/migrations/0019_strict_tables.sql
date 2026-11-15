-- 0019: every table becomes STRICT (ticket 128).
--
-- The rebuild is done in Go (strict.go, migration hook "strict_tables"): each
-- table that is not STRICT yet is rebuilt from its own CREATE TABLE text with
-- STRICT appended, following SQLite's 12-step table rebuild with foreign keys
-- off. Indexes, triggers and AUTOINCREMENT counters are kept; a value whose
-- storage type does not match its column stops the migration instead of being
-- converted. Tables created by later migrations must declare STRICT themselves.
SELECT 1;
