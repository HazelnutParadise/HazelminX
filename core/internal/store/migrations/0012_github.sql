-- 0012: GitHub Apps, GitHub connections and browser authorization requests
-- (ticket 047.1; GIT-001～004, GIT-013～016, GIT-031).
--
-- Secrets (App private key, client secret, webhook secret, PATs, user
-- access and refresh tokens) are never stored here: each *_secret column is
-- the id of a value in the secret store (ticket 011), revealed only to the
-- consumer "github:<row id>". Installation tokens are short-lived and are
-- kept in memory only.
CREATE TABLE IF NOT EXISTS github_apps (
    id                    TEXT PRIMARY KEY,
    name                  TEXT    NOT NULL,
    host                  TEXT    NOT NULL DEFAULT 'github.com',
    app_id                INTEGER NOT NULL,
    slug                  TEXT    NOT NULL,
    client_id             TEXT    NOT NULL,
    owner_login           TEXT    NOT NULL DEFAULT '',
    owner_id              INTEGER NOT NULL DEFAULT 0,
    owner_type            TEXT    NOT NULL DEFAULT '',
    html_url              TEXT    NOT NULL DEFAULT '',
    permissions           TEXT    NOT NULL DEFAULT '{}',  -- JSON {"contents":"read",...}
    events                TEXT    NOT NULL DEFAULT '[]',  -- JSON ["push",...]
    source                TEXT    NOT NULL CHECK (source IN ('manifest', 'manual')),
    private_key_secret    TEXT    NOT NULL,
    client_secret_secret  TEXT    NOT NULL,
    webhook_secret_secret TEXT,
    version               INTEGER NOT NULL DEFAULT 1,
    created_by            TEXT    NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at            TEXT    NOT NULL,   -- UTC RFC 3339
    updated_at            TEXT    NOT NULL,
    UNIQUE (host, app_id)
);

-- One authorization HazelminX may use: a PAT, a GitHub App user
-- authorization, or a GitHub App installation. account_id is GitHub's
-- stable id of the identity behind it; the login is only its current name.
-- Several connections may share an account (GIT-004); a connection belongs
-- to the HazelminX account that created it.
CREATE TABLE IF NOT EXISTS github_connections (
    id                   TEXT PRIMARY KEY,
    owner_user_id        TEXT    NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    name                 TEXT    NOT NULL,
    kind                 TEXT    NOT NULL CHECK (kind IN ('pat', 'app-user', 'app-installation')),
    host                 TEXT    NOT NULL DEFAULT 'github.com',
    app_ref              TEXT REFERENCES github_apps(id) ON DELETE RESTRICT,
    account_id           INTEGER NOT NULL,
    account_login        TEXT    NOT NULL,
    account_type         TEXT    NOT NULL DEFAULT '',
    installation_id      INTEGER,
    repository_selection TEXT    NOT NULL DEFAULT '',
    -- app-installation: the repositories the authorizing user could reach
    -- when the connection was made; every installation token is narrowed
    -- to them, so a connection never reads more than its maker could.
    repository_ids       TEXT    NOT NULL DEFAULT '[]',
    token_type           TEXT    NOT NULL DEFAULT '',  -- classic | fine-grained | app-user | app-installation
    scopes               TEXT    NOT NULL DEFAULT '[]', -- JSON, classic PAT OAuth scopes
    permissions          TEXT    NOT NULL DEFAULT '{}', -- JSON, App permissions
    events               TEXT    NOT NULL DEFAULT '[]',
    state                TEXT    NOT NULL CHECK (state IN ('active', 'disabled', 'needs-reauth')),
    state_reason         TEXT    NOT NULL DEFAULT '',
    access_secret        TEXT,
    refresh_secret       TEXT,
    access_expires_at    TEXT,
    refresh_expires_at   TEXT,
    last_checked_at      TEXT,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TEXT    NOT NULL,
    updated_at           TEXT    NOT NULL,
    CHECK ((kind = 'pat') = (app_ref IS NULL)),
    CHECK ((kind = 'app-installation') = (installation_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS github_connections_owner_idx ON github_connections (owner_user_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS github_connections_installation_idx
    ON github_connections (owner_user_id, app_ref, installation_id) WHERE installation_id IS NOT NULL;

-- One browser round trip to GitHub (App manifest, installation or user
-- authorization). Only the SHA-256 of the state value is stored. A request
-- is bound to the HazelminX account and login session that started it, is
-- valid until expires_at and can be finished once.
CREATE TABLE IF NOT EXISTS github_auth_requests (
    id            TEXT PRIMARY KEY,
    state_hash    TEXT    NOT NULL UNIQUE,
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id    TEXT    NOT NULL,
    purpose       TEXT    NOT NULL CHECK (purpose IN ('manifest', 'authorize', 'install')),
    app_ref       TEXT REFERENCES github_apps(id) ON DELETE CASCADE,
    connection_id TEXT REFERENCES github_connections(id) ON DELETE CASCADE,
    name          TEXT    NOT NULL DEFAULT '',
    redirect_uri  TEXT    NOT NULL,
    doc           TEXT    NOT NULL DEFAULT '{}', -- JSON: manifest request (no secrets)
    created_at    TEXT    NOT NULL,
    expires_at    TEXT    NOT NULL,
    finished_at   TEXT,
    outcome       TEXT CHECK (outcome IN ('completed', 'cancelled', 'failed', 'expired'))
);
CREATE INDEX IF NOT EXISTS github_auth_requests_expires_idx ON github_auth_requests (expires_at);
