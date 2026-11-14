-- 0018: Git sources of Compose units (ticket 049.1; CMP-002, CMP-003,
-- GIT-009, GIT-011, GIT-038, decision 0036).
--
-- A unit whose source is 'git' keeps the files Core took from one fixed
-- commit in compose_unit_files, like any other unit; compose_unit_git_sources
-- says where they came from. A GitHub source keeps a reference to the
-- connection, never a token. A URL source keeps its own credential in the
-- secret store (secret_id, consumer "projects:<unit id>"); an SSH source
-- also keeps the one host key it accepts.
--
-- SQLite cannot change a CHECK in place, so compose_units is rebuilt to
-- allow source 'git'. Its children are rebuilt first and dropped before it,
-- so dropping the old table cascades to nothing.

CREATE TABLE compose_units_v2 (
    id               TEXT PRIMARY KEY,
    environment_id   TEXT    NOT NULL UNIQUE REFERENCES project_environments(id) ON DELETE RESTRICT,
    name             TEXT    NOT NULL,
    compose_project  TEXT    NOT NULL UNIQUE,
    source           TEXT    NOT NULL CHECK (source IN ('upload', 'editor', 'git')),
    main_file        TEXT    NOT NULL,
    extra_files      TEXT    NOT NULL DEFAULT '[]',
    working_dir      TEXT    NOT NULL DEFAULT '',
    profiles         TEXT    NOT NULL DEFAULT '[]',
    node_id          TEXT    REFERENCES nodes(id) ON DELETE RESTRICT,
    engine_env_id    TEXT,
    version          INTEGER NOT NULL DEFAULT 1,
    created_by       TEXT    NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at       TEXT    NOT NULL,
    updated_at       TEXT    NOT NULL,
    CHECK ((node_id IS NULL) = (engine_env_id IS NULL))
);
INSERT INTO compose_units_v2 (id, environment_id, name, compose_project, source, main_file, extra_files, working_dir,
    profiles, node_id, engine_env_id, version, created_by, created_at, updated_at)
SELECT id, environment_id, name, compose_project, source, main_file, extra_files, working_dir,
    profiles, node_id, engine_env_id, version, created_by, created_at, updated_at FROM compose_units;

CREATE TABLE compose_unit_files_v2 (
    unit_id   TEXT    NOT NULL REFERENCES compose_units_v2(id) ON DELETE CASCADE,
    path      TEXT    NOT NULL,
    content   BLOB,
    secret_id TEXT,
    size      INTEGER NOT NULL,
    sha256    TEXT    NOT NULL,
    PRIMARY KEY (unit_id, path),
    CHECK ((content IS NULL) <> (secret_id IS NULL))
);
INSERT INTO compose_unit_files_v2 (unit_id, path, content, secret_id, size, sha256)
SELECT unit_id, path, content, secret_id, size, sha256 FROM compose_unit_files;

-- source_* say which commit a deployment of a Git unit ran; empty for
-- other units.
CREATE TABLE compose_deployments_v2 (
    id               TEXT PRIMARY KEY,
    unit_id          TEXT    NOT NULL REFERENCES compose_units_v2(id) ON DELETE CASCADE,
    unit_version     INTEGER NOT NULL,
    compose_project  TEXT    NOT NULL,
    revision         TEXT    NOT NULL UNIQUE,
    manifest_digest  TEXT    NOT NULL,
    node_id          TEXT    NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    engine_env_id    TEXT    NOT NULL,
    job_id           TEXT    NOT NULL UNIQUE,
    plan_id          TEXT    NOT NULL DEFAULT '',
    state            TEXT    NOT NULL CHECK (state IN ('running', 'succeeded', 'failed')),
    result           TEXT    NOT NULL DEFAULT '{}',
    error_code       TEXT    NOT NULL DEFAULT '',
    error_message    TEXT    NOT NULL DEFAULT '',
    created_by       TEXT    NOT NULL,
    started_at       TEXT    NOT NULL,
    finished_at      TEXT,
    source_repo      TEXT    NOT NULL DEFAULT '',  -- GitHub full name or URL without credentials
    source_ref       TEXT    NOT NULL DEFAULT '',
    source_commit    TEXT    NOT NULL DEFAULT ''   -- 40 hex characters
);
INSERT INTO compose_deployments_v2 (id, unit_id, unit_version, compose_project, revision, manifest_digest, node_id,
    engine_env_id, job_id, plan_id, state, result, error_code, error_message, created_by, started_at, finished_at)
SELECT id, unit_id, unit_version, compose_project, revision, manifest_digest, node_id,
    engine_env_id, job_id, plan_id, state, result, error_code, error_message, created_by, started_at, finished_at
FROM compose_deployments;

DROP TABLE compose_deployments;
DROP TABLE compose_unit_files;
DROP TABLE compose_units;
ALTER TABLE compose_units_v2 RENAME TO compose_units;
ALTER TABLE compose_unit_files_v2 RENAME TO compose_unit_files;
ALTER TABLE compose_deployments_v2 RENAME TO compose_deployments;
CREATE INDEX compose_deployments_unit ON compose_deployments (unit_id, started_at);
CREATE INDEX compose_deployments_node ON compose_deployments (node_id, state);

CREATE TABLE compose_unit_git_sources (
    unit_id        TEXT    PRIMARY KEY REFERENCES compose_units(id) ON DELETE CASCADE,
    kind           TEXT    NOT NULL CHECK (kind IN ('github', 'url')),
    -- github: the connection (ticket 047.1) and GitHub's repository id; the
    -- full name is what it was called when the commit was fetched.
    connection_id  TEXT    NOT NULL DEFAULT '',
    repo_id        INTEGER NOT NULL DEFAULT 0,
    repo_name      TEXT    NOT NULL DEFAULT '',
    -- url: https:// or ssh:// without user information; host is the
    -- lowercased host name it names.
    url            TEXT    NOT NULL DEFAULT '',
    host           TEXT    NOT NULL,
    auth           TEXT    NOT NULL CHECK (auth IN ('connection', 'none', 'token', 'ssh-key')),
    username       TEXT    NOT NULL DEFAULT '',  -- HTTPS user name sent with a token, or the SSH user
    secret_id      TEXT,                         -- token or private key; NULL for 'connection' and 'none'
    host_key       TEXT    NOT NULL DEFAULT '',  -- the SSH host key accepted, authorized_keys form
    ref            TEXT    NOT NULL,             -- as asked: branch, tag or commit
    commit_sha     TEXT    NOT NULL,
    digest         TEXT    NOT NULL,             -- sha256 of the source binding (decision 0036)
    fetched_by     TEXT    NOT NULL,
    fetched_at     TEXT    NOT NULL,             -- UTC RFC 3339
    CHECK ((kind = 'github') = (auth = 'connection')),
    CHECK ((secret_id IS NULL) = (auth IN ('connection', 'none')))
);
CREATE INDEX compose_unit_git_sources_connection ON compose_unit_git_sources (connection_id);
