-- 0017: deployments of Compose units (ticket 044.2; CMP-007, CMP-008,
-- CMP-012, decision 0035).
--
-- One row per deployment job. revision names the directory the executor
-- wrote under /var/lib/hazelminx/compose/<compose_project>/revisions/;
-- manifest_digest is what the job's grants were bound to. node_id and
-- engine_env_id are copied from the unit when the deployment starts, so a
-- later change of the unit's target does not rewrite where it ran. Rows are
-- never deleted while the unit exists; they are the unit's deployment
-- history until ticket 046 adds revisions and rollback.
CREATE TABLE IF NOT EXISTS compose_deployments (
    id               TEXT PRIMARY KEY,
    unit_id          TEXT    NOT NULL REFERENCES compose_units(id) ON DELETE CASCADE,
    unit_version     INTEGER NOT NULL,
    compose_project  TEXT    NOT NULL,
    revision         TEXT    NOT NULL UNIQUE,
    manifest_digest  TEXT    NOT NULL,
    node_id          TEXT    NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    engine_env_id    TEXT    NOT NULL,
    job_id           TEXT    NOT NULL UNIQUE,
    plan_id          TEXT    NOT NULL DEFAULT '',
    state            TEXT    NOT NULL CHECK (state IN ('running', 'succeeded', 'failed')),
    -- JSON of the executor's answer: checks, services, containers. No file
    -- content and no secret value is ever in it.
    result           TEXT    NOT NULL DEFAULT '{}',
    error_code       TEXT    NOT NULL DEFAULT '',
    error_message    TEXT    NOT NULL DEFAULT '',
    created_by       TEXT    NOT NULL,
    started_at       TEXT    NOT NULL,   -- UTC RFC 3339
    finished_at      TEXT
);

CREATE INDEX IF NOT EXISTS compose_deployments_unit ON compose_deployments (unit_id, started_at);
CREATE INDEX IF NOT EXISTS compose_deployments_node ON compose_deployments (node_id, state);
