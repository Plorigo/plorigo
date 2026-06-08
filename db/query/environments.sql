-- name: CreateEnvironment :one
INSERT INTO environments (project_id, name, slug, type)
VALUES ($1, $2, $3, $4)
RETURNING id, project_id, name, slug, type, created_at;

-- name: GetEnvironment :one
-- Joins the parent project so the caller can authorize against the owning
-- workspace (environments are project-scoped; authorization is workspace-scoped).
SELECT e.id, e.project_id, e.name, e.slug, e.type, e.created_at, p.workspace_id
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE e.id = $1;

-- name: ListEnvironmentsByProject :many
SELECT id, project_id, name, slug, type, created_at
FROM environments
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: GetProjectWorkspaceID :one
-- Resolves a project's owning workspace, so a project-scoped action can be
-- authorized and audited against the workspace.
SELECT workspace_id FROM projects WHERE id = $1;
