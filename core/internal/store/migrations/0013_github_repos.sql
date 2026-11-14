-- 0013: repository inventory of GitHub connections for the source picker
-- (ticket 048.1; GIT-005～008, GIT-034).
--
-- Every row belongs to exactly one connection: the same repository reached
-- through two connections is two rows, so a list may show them together
-- without merging what each connection may do. Removing a connection
-- removes its inventory.

-- One source of a connection's repositories (the user's own and
-- collaborator repositories, one organization, or an App installation),
-- each with its own cursor so one failing does not hide or complete the
-- others.
CREATE TABLE IF NOT EXISTS github_repo_sources (
    connection_id TEXT    NOT NULL REFERENCES github_connections(id) ON DELETE CASCADE,
    source_key    TEXT    NOT NULL,              -- user | installation | org:<login>
    kind          TEXT    NOT NULL CHECK (kind IN ('user', 'org', 'installation')),
    login         TEXT    NOT NULL DEFAULT '',
    state         TEXT    NOT NULL CHECK (state IN ('pending', 'syncing', 'complete', 'failed', 'rate-limited')),
    next_page     INTEGER NOT NULL DEFAULT 1,    -- page the next sync step reads
    error         TEXT    NOT NULL DEFAULT '',   -- why the source failed; never a secret
    retry_at      TEXT,                          -- UTC RFC 3339; no GitHub call for this connection before it
    synced_at     TEXT,                          -- last time the source was read to its end
    -- generation: what the connection was allowed to see when this round
    -- started (account, kind, installation, scopes, permissions,
    -- repositories, expiry). A source of another generation is stale: shown,
    -- marked, never complete, and re-read by the next sync step.
    generation    TEXT    NOT NULL DEFAULT '',
    updated_at    TEXT    NOT NULL,
    PRIMARY KEY (connection_id, source_key)
);

-- What GitHub answered for each page, so the next sync sends If-None-Match
-- and reuses the stored rows on 304.
CREATE TABLE IF NOT EXISTS github_repo_pages (
    connection_id TEXT    NOT NULL,
    source_key    TEXT    NOT NULL,
    page          INTEGER NOT NULL,
    etag          TEXT    NOT NULL DEFAULT '',
    has_next      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (connection_id, source_key, page),
    FOREIGN KEY (connection_id, source_key) REFERENCES github_repo_sources(connection_id, source_key) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS github_repositories (
    connection_id  TEXT    NOT NULL,
    source_key     TEXT    NOT NULL,
    page           INTEGER NOT NULL,
    repo_id        INTEGER NOT NULL,             -- GitHub's stable id
    full_name      TEXT    NOT NULL,
    name           TEXT    NOT NULL,
    owner_id       INTEGER NOT NULL,
    owner_login    TEXT    NOT NULL,
    owner_type     TEXT    NOT NULL DEFAULT '',
    private        INTEGER NOT NULL,
    visibility     TEXT    NOT NULL,
    archived       INTEGER NOT NULL,
    disabled       INTEGER NOT NULL,
    fork           INTEGER NOT NULL,
    default_branch TEXT    NOT NULL DEFAULT '',
    html_url       TEXT    NOT NULL DEFAULT '',
    pushed_at      TEXT,
    size_kb        INTEGER NOT NULL DEFAULT 0,
    can_push       INTEGER NOT NULL DEFAULT 0,
    can_admin      INTEGER NOT NULL DEFAULT 0,
    seen_at        TEXT    NOT NULL,
    PRIMARY KEY (connection_id, source_key, repo_id),
    FOREIGN KEY (connection_id, source_key) REFERENCES github_repo_sources(connection_id, source_key) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS github_repositories_repo ON github_repositories(connection_id, repo_id);
