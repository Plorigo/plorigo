-- name: UpsertEnvVar :one
-- Create-or-update by (environment_id, key). The conflict is the success path (env
-- vars are mutable config), so callers never see an AlreadyExists error. RETURNING
-- yields the row id on both the insert and the update path, for the audit target.
INSERT INTO env_vars (environment_id, key, value)
VALUES ($1, $2, $3)
ON CONFLICT (environment_id, key)
DO UPDATE SET value = EXCLUDED.value, updated_at = now()
RETURNING id, environment_id, key, value, created_at, updated_at;

-- name: ListEnvVarsByEnvironment :many
SELECT id, environment_id, key, value, created_at, updated_at
FROM env_vars
WHERE environment_id = $1
ORDER BY key;

-- name: DeleteEnvVar :one
-- RETURNING id lets the caller tell a real delete from a no-op (no row -> ErrNoRows
-- -> NotFound), so a delete that removed nothing is never audited as a change.
DELETE FROM env_vars
WHERE environment_id = $1 AND key = $2
RETURNING id;

-- name: GetEnvironmentWorkspaceID :one
-- Resolves an environment's owning workspace through its parent project, so this
-- environment-scoped module can authorize and audit against the workspace.
SELECT p.workspace_id
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE e.id = $1;
