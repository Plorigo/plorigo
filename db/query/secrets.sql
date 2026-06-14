-- name: UpsertSecret :one
-- Create-or-update by (environment_id, key). The conflict is the success path
-- (secrets are mutable), so callers never see an AlreadyExists error. RETURNING
-- yields metadata only — never the ciphertext, which is write-only and leaves the
-- database only for a future deploy job, never through the API.
INSERT INTO secrets (environment_id, key, ciphertext)
VALUES ($1, $2, $3)
ON CONFLICT (environment_id, key)
DO UPDATE SET ciphertext = EXCLUDED.ciphertext, updated_at = now()
RETURNING id, environment_id, key, created_at, updated_at;

-- name: ListSecretsByEnvironment :many
-- Metadata only — keys and timestamps, never the ciphertext (write-only).
SELECT id, environment_id, key, created_at, updated_at
FROM secrets
WHERE environment_id = $1
ORDER BY key;

-- name: DeleteSecret :one
-- RETURNING id lets the caller tell a real delete from a no-op (no row -> ErrNoRows
-- -> NotFound), so a delete that removed nothing is never audited as a change.
DELETE FROM secrets
WHERE environment_id = $1 AND key = $2
RETURNING id;

-- name: GetEnvironmentWorkspaceID :one
-- Resolves an environment's owning workspace through its parent project, so this
-- environment-scoped module can authorize and audit against the workspace. (Secrets stay
-- environment-scoped; env vars moved to per-service, so this query lives here now.)
SELECT p.workspace_id
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE e.id = $1;
