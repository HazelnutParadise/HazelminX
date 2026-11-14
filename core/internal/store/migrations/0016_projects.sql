-- 0016: projects, project environments and Compose units (ticket 044.1;
-- CMP-001, CMP-015, addendum 04 52.1).
--
-- A unit's files are kept byte for byte as uploaded. A file marked secret
-- has no content here: secret_id is the id of its value in the secret store
-- (ticket 011), revealed only to the consumer "projects:<unit id>".
-- compose_project is fixed when the unit is created and never changes, so
-- renaming does not move the unit's networks and volumes (52.1).
CREATE TABLE IF NOT EXISTS projects (
    id           TEXT PRIMARY KEY,
    name         TEXT    NOT NULL UNIQUE,
    display_name TEXT    NOT NULL DEFAULT '',
    description  TEXT    NOT NULL DEFAULT '',
    version      INTEGER NOT NULL DEFAULT 1,
    created_by   TEXT    NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at   TEXT    NOT NULL,   -- UTC RFC 3339
    updated_at   TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS project_environments (
    id          TEXT PRIMARY KEY,
    project_id  TEXT    NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    version     INTEGER NOT NULL DEFAULT 1,
    created_by  TEXT    NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL,
    UNIQUE (project_id, name)
);

-- One unit per environment until ticket 108 allows several.
CREATE TABLE IF NOT EXISTS compose_units (
    id               TEXT PRIMARY KEY,
    environment_id   TEXT    NOT NULL UNIQUE REFERENCES project_environments(id) ON DELETE RESTRICT,
    name             TEXT    NOT NULL,
    compose_project  TEXT    NOT NULL UNIQUE,
    source           TEXT    NOT NULL CHECK (source IN ('upload', 'editor')),
    main_file        TEXT    NOT NULL,
    extra_files      TEXT    NOT NULL DEFAULT '[]',  -- JSON ["deploy/prod.yml",...], merge order after main_file
    working_dir      TEXT    NOT NULL DEFAULT '',    -- relative project directory; '' = main file's directory
    profiles         TEXT    NOT NULL DEFAULT '[]',  -- JSON ["debug",...]
    node_id          TEXT    REFERENCES nodes(id) ON DELETE RESTRICT,
    engine_env_id    TEXT,                           -- environments[].id of the node's engine report
    version          INTEGER NOT NULL DEFAULT 1,
    created_by       TEXT    NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at       TEXT    NOT NULL,
    updated_at       TEXT    NOT NULL,
    CHECK ((node_id IS NULL) = (engine_env_id IS NULL))
);

CREATE TABLE IF NOT EXISTS compose_unit_files (
    unit_id   TEXT    NOT NULL REFERENCES compose_units(id) ON DELETE CASCADE,
    path      TEXT    NOT NULL,   -- relative to the unit's file root, '/'-separated
    content   BLOB,               -- NULL when secret_id is set
    secret_id TEXT,
    size      INTEGER NOT NULL,
    sha256    TEXT    NOT NULL,
    PRIMARY KEY (unit_id, path),
    CHECK ((content IS NULL) <> (secret_id IS NULL))
);
