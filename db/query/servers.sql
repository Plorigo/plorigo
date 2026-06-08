-- name: CreateServer :one
INSERT INTO servers (workspace_id, name, slug)
VALUES ($1, $2, $3)
RETURNING id, workspace_id, name, slug, created_at;

-- name: GetServer :one
SELECT id, workspace_id, name, slug, created_at
FROM servers
WHERE id = $1;

-- name: ListServersByWorkspace :many
SELECT id, workspace_id, name, slug, created_at
FROM servers
WHERE workspace_id = $1
ORDER BY created_at DESC;
