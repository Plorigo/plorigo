-- name: UpsertEnvVar :one
-- Create-or-update by (service_id, key). The conflict is the success path (env vars are
-- mutable config), so callers never see an AlreadyExists error. RETURNING yields the row id
-- on both the insert and the update path, for the audit target.
INSERT INTO env_vars (service_id, key, value)
VALUES ($1, $2, $3)
ON CONFLICT (service_id, key)
DO UPDATE SET value = EXCLUDED.value, updated_at = now()
RETURNING *;

-- name: ListEnvVarsByService :many
SELECT * FROM env_vars
WHERE service_id = $1
ORDER BY key;

-- name: DeleteEnvVar :one
-- RETURNING id lets the caller tell a real delete from a no-op (no row -> ErrNoRows
-- -> NotFound), so a delete that removed nothing is never audited as a change.
DELETE FROM env_vars
WHERE service_id = $1 AND key = $2
RETURNING id;

-- name: GetServiceWorkspaceID :one
-- Resolves a service's owning workspace (denormalized onto the service row), so this
-- service-scoped module can authorize and audit against the workspace.
SELECT workspace_id FROM services WHERE id = $1;
