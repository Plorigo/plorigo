-- +goose Up
-- A workspace's connection to a Git provider account (GitHub via OAuth in this slice),
-- one per (workspace, provider). The OAuth access token is stored as ciphertext
-- (AES-256-GCM, sealed by the control plane with APP_MASTER_KEY). Unlike a secret it is
-- opened server-side to call the provider on the user's behalf, but it is still
-- WRITE-ONLY through the API: never returned by any RPC and never logged. See
-- docs/architecture/security.md and data-and-api.md.
CREATE TABLE source_connections (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider       text NOT NULL DEFAULT 'github'
        CHECK (provider IN ('github')),
    -- The authenticated account login (e.g. the GitHub username/org), for display.
    github_login   text NOT NULL,
    -- The provider's stable numeric account id, when known.
    github_user_id bigint,
    -- Ciphertext only — never plaintext. The CHECK bounds the sealed bytes
    -- (nonce + ciphertext + GCM tag) as a backstop (cf. 00007_secrets.sql).
    access_token_ciphertext bytea NOT NULL
        CHECK (octet_length(access_token_ciphertext) <= 65536),
    scopes         text NOT NULL DEFAULT '',
    -- The user who authorized the connection; kept for display/audit. SET NULL on
    -- user deletion so the connection (and the projects using it) survive.
    connected_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, provider) -- one connection per provider per workspace
);

CREATE INDEX idx_source_connections_workspace_id ON source_connections (workspace_id);

-- A project's connected repository + branch, resolved through the workspace's
-- connection. One source per project. Building/cloning from it is a later slice; this
-- table records the selection and the repo metadata shown in the dashboard.
CREATE TABLE project_sources (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    -- RESTRICT, not CASCADE: disconnecting a workspace's provider while projects still
    -- use it must fail loudly (a recovery path), not silently drop their sources.
    connection_id  uuid NOT NULL REFERENCES source_connections (id) ON DELETE RESTRICT,
    provider       text NOT NULL DEFAULT 'github'
        CHECK (provider IN ('github')),
    owner          text NOT NULL,
    repo           text NOT NULL,
    full_name      text NOT NULL,
    branch         text NOT NULL,
    default_branch text NOT NULL DEFAULT '',
    is_private     boolean NOT NULL DEFAULT false,
    html_url       text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id) -- one source per project
);

CREATE INDEX idx_project_sources_connection_id ON project_sources (connection_id);

-- +goose Down
DROP TABLE IF EXISTS project_sources;
DROP TABLE IF EXISTS source_connections;
