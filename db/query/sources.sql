-- name: ListConnectionsByWorkspace :many
-- All integrations (connections) for a workspace, newest first. Metadata only — never the sealed
-- token, which is write-only and leaves the database only to call the provider server-side.
SELECT id, workspace_id, provider, kind, account_login, account_id, installation_id, scopes, connected_by, created_at, updated_at
FROM source_connections
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: GetConnectionByID :one
-- One integration's metadata by id (to authorize via its workspace + display it). Never the token.
SELECT id, workspace_id, provider, kind, account_login, account_id, installation_id, scopes, connected_by, created_at, updated_at
FROM source_connections
WHERE id = $1;

-- name: GetSealedTokenByConnection :one
-- The sealed OAuth token for one connection, for server-side provider calls. INTERNAL — the
-- ciphertext is never wired to a handler or returned by any RPC.
SELECT access_token_ciphertext
FROM source_connections
WHERE id = $1;

-- name: GetInstallationByConnection :one
-- The App installation id for one connection, to mint a per-installation token. INTERNAL — the id
-- resolves a token that is never returned by an RPC.
SELECT installation_id
FROM source_connections
WHERE id = $1;

-- name: InsertOAuthConnection :one
-- Create-or-refresh an OAuth integration. Reconnecting the SAME account (workspace+provider+kind+
-- account_id) refreshes the row via the partial unique index, so callers never see AlreadyExists; a
-- different account adds another integration. RETURNING is metadata only — never the token.
INSERT INTO source_connections (
    workspace_id, provider, kind, account_login, account_id, access_token_ciphertext, scopes, connected_by
)
VALUES ($1, $2, 'oauth', $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, provider, kind, account_id) WHERE account_id IS NOT NULL
DO UPDATE SET
    account_login = EXCLUDED.account_login,
    access_token_ciphertext = EXCLUDED.access_token_ciphertext,
    scopes = EXCLUDED.scopes,
    connected_by = EXCLUDED.connected_by,
    updated_at = now()
RETURNING id, workspace_id, provider, kind, account_login, account_id, installation_id, scopes, connected_by, created_at, updated_at;

-- name: InsertAppConnection :one
-- Create-or-refresh an App-installation integration. The installation id is globally unique, so a
-- re-install of the same installation refreshes the existing row (and re-homes it to this workspace)
-- via the partial unique index. RETURNING is metadata only.
INSERT INTO source_connections (
    workspace_id, provider, kind, account_login, account_id, installation_id, connected_by
)
VALUES ($1, $2, 'app', $3, $4, $5, $6)
ON CONFLICT (provider, installation_id) WHERE installation_id IS NOT NULL
DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    account_login = EXCLUDED.account_login,
    account_id = EXCLUDED.account_id,
    connected_by = EXCLUDED.connected_by,
    updated_at = now()
RETURNING id, workspace_id, provider, kind, account_login, account_id, installation_id, scopes, connected_by, created_at, updated_at;

-- name: DeleteConnectionByID :one
-- RETURNING id distinguishes a real delete from a no-op (no row -> ErrNoRows -> NotFound), so a
-- delete that removed nothing is never audited as a change.
DELETE FROM source_connections
WHERE id = $1
RETURNING id;

-- A service's connected repository lives on the services table (folded in, see db/query/services.sql),
-- referencing the chosen connection by services.connection_id. The guard that a connection is still
-- in use is CountServicesByConnection (db/query/services.sql), read as a sibling-table read.
