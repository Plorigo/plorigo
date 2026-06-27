-- name: CreateServer :one
INSERT INTO servers (workspace_id, name, slug)
VALUES ($1, $2, $3)
RETURNING id, workspace_id, name, slug, created_at, host_key_fingerprint;

-- name: GetServer :one
SELECT id, workspace_id, name, slug, created_at, host_key_fingerprint
FROM servers
WHERE id = $1;

-- name: ListServersByWorkspace :many
SELECT id, workspace_id, name, slug, created_at, host_key_fingerprint
FROM servers
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- DeleteServer removes the server (agents, registration tokens, and deployments
-- cascade). RETURNING id lets the caller tell a real delete from a no-op, so a delete
-- that removed nothing is never audited as a change (cf. DeleteEnvVar).
-- name: DeleteServer :one
DELETE FROM servers
WHERE id = $1
RETURNING id;
