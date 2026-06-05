-- name: CreateProject :one
INSERT INTO projects (workspace_id, name, slug)
VALUES ($1, $2, $3)
RETURNING id, workspace_id, name, slug, created_at;

-- name: GetProject :one
SELECT id, workspace_id, name, slug, created_at
FROM projects
WHERE id = $1;

-- name: ListProjectsByWorkspace :many
SELECT id, workspace_id, name, slug, created_at
FROM projects
WHERE workspace_id = $1
ORDER BY created_at DESC;
