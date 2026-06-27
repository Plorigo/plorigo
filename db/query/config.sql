-- name: UpsertServiceConfig :one
-- Create-or-update a service-scoped entry by (service_id, key). The conflict is the
-- success path (config is mutable), so callers never see an AlreadyExists error. value is
-- set for variables, ciphertext for secrets (the other is NULL). RETURNING yields the row
-- for the audit target; the handler never returns a secret value over the wire.
INSERT INTO config_entries (type, scope, service_id, key, value, ciphertext)
VALUES ($1, 'service', $2, $3, $4, $5)
ON CONFLICT (service_id, key) WHERE service_id IS NOT NULL
DO UPDATE SET type = EXCLUDED.type, value = EXCLUDED.value, ciphertext = EXCLUDED.ciphertext, updated_at = now()
RETURNING *;

-- name: UpsertEnvironmentConfig :one
-- Create-or-update an environment-scoped (shared) entry by (environment_id, key).
INSERT INTO config_entries (type, scope, environment_id, key, value, ciphertext)
VALUES ($1, 'environment', $2, $3, $4, $5)
ON CONFLICT (environment_id, key) WHERE environment_id IS NOT NULL
DO UPDATE SET type = EXCLUDED.type, value = EXCLUDED.value, ciphertext = EXCLUDED.ciphertext, updated_at = now()
RETURNING *;

-- name: ListConfigForService :many
-- Everything that applies to a service: its own service-level entries plus the
-- environment-shared entries for the service's environment (resolved via the subquery).
-- Ordered environment-first (scope ASC) so a single-pass merge lets service-level entries
-- override environment-shared ones, then by key. Used by both the dashboard list and the
-- deploy-time injection.
SELECT * FROM config_entries
WHERE service_id = sqlc.arg(service_id)
   OR environment_id = (SELECT environment_id FROM services WHERE id = sqlc.arg(service_id))
ORDER BY scope, key;

-- name: DeleteServiceConfig :one
-- RETURNING id lets the caller tell a real delete from a no-op (no row -> ErrNoRows ->
-- NotFound), so a delete that removed nothing is never audited as a change.
DELETE FROM config_entries
WHERE service_id = $1 AND key = $2
RETURNING id;

-- name: DeleteEnvironmentConfig :one
DELETE FROM config_entries
WHERE environment_id = $1 AND key = $2
RETURNING id;

-- name: GetServiceWorkspaceID :one
-- Resolves a service's owning workspace (denormalized onto the service row), so the
-- service-scoped config path can authorize and audit against the workspace.
SELECT workspace_id FROM services WHERE id = $1;

-- name: GetEnvironmentWorkspaceID :one
-- Resolves an environment's owning workspace through its parent project, so the
-- environment-scoped config path can authorize and audit against the workspace.
SELECT p.workspace_id
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE e.id = $1;
