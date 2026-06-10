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

-- name: CountProjectSourcesByConnection :one
-- Guards DisconnectGitHub: a connection still in use by projects must not be removed.
SELECT count(*) FROM project_sources WHERE connection_id = $1;

-- name: UpsertProjectSource :one
-- Create-or-update by project_id (one source per project). The conflict is the success
-- path (reconnecting a project to a different repo or branch).
INSERT INTO project_sources (
    project_id, connection_id, provider, owner, repo, full_name, branch, default_branch, is_private, html_url
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (project_id)
DO UPDATE SET
    connection_id = EXCLUDED.connection_id,
    provider = EXCLUDED.provider,
    owner = EXCLUDED.owner,
    repo = EXCLUDED.repo,
    full_name = EXCLUDED.full_name,
    branch = EXCLUDED.branch,
    default_branch = EXCLUDED.default_branch,
    is_private = EXCLUDED.is_private,
    html_url = EXCLUDED.html_url,
    updated_at = now()
RETURNING id, project_id, connection_id, provider, owner, repo, full_name, branch, default_branch, is_private, html_url, created_at, updated_at;

-- name: GetProjectSource :one
-- Joins the connection for the account login (display). Workspace resolution for
-- authorization uses the shared GetProjectWorkspaceID.
SELECT
    ps.id, ps.project_id, ps.connection_id, ps.provider, ps.owner, ps.repo, ps.full_name,
    ps.branch, ps.default_branch, ps.is_private, ps.html_url, ps.created_at, ps.updated_at,
    sc.github_login
FROM project_sources ps
JOIN source_connections sc ON sc.id = ps.connection_id
WHERE ps.project_id = $1;

-- name: ListProjectSourcesByWorkspace :many
-- Batch read for the projects grid (avoids an N+1 over GetProjectSource). Joins the
-- parent project to scope by workspace and the connection for the account login.
SELECT
    ps.id, ps.project_id, ps.connection_id, ps.provider, ps.owner, ps.repo, ps.full_name,
    ps.branch, ps.default_branch, ps.is_private, ps.html_url, ps.created_at, ps.updated_at,
    sc.github_login
FROM project_sources ps
JOIN source_connections sc ON sc.id = ps.connection_id
JOIN projects p ON p.id = ps.project_id
WHERE p.workspace_id = $1
ORDER BY ps.updated_at DESC;

-- name: DeleteProjectSource :one
-- RETURNING id distinguishes a real delete from a no-op (NotFound).
DELETE FROM project_sources
WHERE project_id = $1
RETURNING id;
