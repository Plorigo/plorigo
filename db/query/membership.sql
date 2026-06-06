-- name: CreateWorkspace :one
INSERT INTO workspaces (name, slug)
VALUES ($1, $2)
RETURNING id, name, slug, created_at;

-- name: GetWorkspaceMemberRole :one
SELECT role
FROM workspace_members
WHERE workspace_id = $1 AND user_id = $2;

-- name: AddWorkspaceMember :exec
INSERT INTO workspace_members (workspace_id, user_id, role)
VALUES ($1, $2, $3);

-- name: UpdateMemberRole :exec
UPDATE workspace_members SET role = $3
WHERE workspace_id = $1 AND user_id = $2;

-- name: RemoveMember :exec
DELETE FROM workspace_members
WHERE workspace_id = $1 AND user_id = $2;

-- name: ListWorkspacesForUser :many
SELECT w.id, w.name, w.slug, w.created_at
FROM workspaces w
JOIN workspace_members wm ON wm.workspace_id = w.id
WHERE wm.user_id = $1
ORDER BY w.created_at;

-- name: ListMembers :many
SELECT wm.user_id, u.email, wm.role, wm.created_at
FROM workspace_members wm
JOIN users u ON u.id = wm.user_id
WHERE wm.workspace_id = $1
ORDER BY wm.created_at;
