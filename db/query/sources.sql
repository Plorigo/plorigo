-- name: UpsertSourceConnection :one
-- Create-or-update the workspace's provider connection (one per workspace + provider).
-- The conflict is the success path (reconnecting refreshes the token and identity), so
-- callers never see AlreadyExists. RETURNING yields metadata only — never the token,
-- which is write-only and leaves the database only to call the provider server-side.
INSERT INTO source_connections (
    workspace_id, provider, github_login, github_user_id, access_token_ciphertext, scopes, connected_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, provider)
DO UPDATE SET
    github_login = EXCLUDED.github_login,
    github_user_id = EXCLUDED.github_user_id,
    access_token_ciphertext = EXCLUDED.access_token_ciphertext,
    scopes = EXCLUDED.scopes,
    connected_by = EXCLUDED.connected_by,
    updated_at = now()
RETURNING id, workspace_id, provider, github_login, github_user_id, scopes, connected_by, created_at, updated_at;

-- name: GetSourceConnectionByWorkspace :one
-- Metadata only — never the access token (write-only).
SELECT id, workspace_id, provider, github_login, github_user_id, scopes, connected_by, created_at, updated_at
FROM source_connections
WHERE workspace_id = $1 AND provider = $2;

-- name: GetConnectionTokenByWorkspace :one
-- Returns the sealed token for server-side provider calls. INTERNAL — the ciphertext
-- is never wired to a handler or returned by any RPC.
SELECT access_token_ciphertext
FROM source_connections
WHERE workspace_id = $1 AND provider = $2;

-- name: DeleteSourceConnection :one
-- RETURNING id distinguishes a real delete from a no-op (no row -> ErrNoRows ->
-- NotFound), so a delete that removed nothing is never audited as a change.
DELETE FROM source_connections
WHERE workspace_id = $1 AND provider = $2
RETURNING id;

-- A service's connected repository lives on the services table now (folded in, see
-- db/query/services.sql). The guard that a connection is still in use is
-- CountServicesByConnection, which the sources module reads as a sibling-table read.
