-- A service is a deployable component of a project, living in one environment, owning its
-- source (folded onto the row), port, visibility, and deployment history. See
-- docs/architecture/deployment-engine.md and modules.md.

-- name: CreateService :one
-- Image or template service: no git columns. workspace_id/project_id are denormalized from
-- the environment by the caller (both immutable).
INSERT INTO services (
    environment_id, project_id, workspace_id, name, slug,
    source_kind, image_ref, template_id, container_port, visibility
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: CreateGitService :one
-- Git service: the source is a repo+ref. connection_id is NULL for a public repo; source_access
-- records how it is reached ('public' this slice; 'oauth'/'app' recorded but not yet buildable).
INSERT INTO services (
    environment_id, project_id, workspace_id, name, slug,
    source_kind, source_access, connection_id, provider, owner, repo, full_name,
    branch, default_branch, is_private, html_url, container_port, visibility
)
VALUES ($1, $2, $3, $4, $5, 'git', $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
RETURNING *;

-- name: GetService :one
SELECT * FROM services WHERE id = $1;

-- GetServiceForDeploy reads the source + routing facts a deployment needs, so the deployments
-- module can enqueue a deploy for a service without importing services (a sibling-table read,
-- which modules.md Rule 2 permits from a module's postgres.go).
-- name: GetServiceForDeploy :one
SELECT
    environment_id, project_id, workspace_id, source_kind, image_ref,
    source_access, connection_id, owner, repo, full_name, branch, default_branch, html_url,
    container_port, visibility, slug
FROM services WHERE id = $1;

-- name: ListServicesByEnvironment :many
SELECT * FROM services WHERE environment_id = $1 ORDER BY created_at DESC;

-- name: ListServicesByProject :many
SELECT * FROM services WHERE project_id = $1 ORDER BY created_at DESC;

-- name: ListServicesByWorkspace :many
SELECT * FROM services WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: UpdateServiceSource :one
-- Reconnect/change a service's source (and port). Updates every source-related column so a
-- service can switch kind (image<->git<->template).
UPDATE services
SET source_kind = $2, image_ref = $3, template_id = $4, connection_id = $5,
    provider = $6, owner = $7, repo = $8, full_name = $9, branch = $10,
    default_branch = $11, is_private = $12, html_url = $13, source_access = $14,
    container_port = $15, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateServiceVisibility :one
UPDATE services SET visibility = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- UpdateServiceRouteURL keeps the service's cached public URL current from the latest running
-- deployment's reported route. Only called for a public service with a real route.
-- name: UpdateServiceRouteURL :exec
UPDATE services SET route_url = $2, updated_at = now() WHERE id = $1;

-- name: DeleteService :one
-- RETURNING id distinguishes a real delete from a no-op (no row -> ErrNoRows -> NotFound).
DELETE FROM services WHERE id = $1 RETURNING id;

-- GetServiceWorkspaceAndProject resolves a service's owning workspace and project (both
-- denormalized onto the row) for authorization and scoping.
-- name: GetServiceWorkspaceAndProject :one
SELECT workspace_id, project_id FROM services WHERE id = $1;

-- CountServicesByConnection guards DisconnectGitHub: a connection still used by services must
-- not be removed. Read by the sources module as a sibling-table read (modules.md Rule 2).
-- name: CountServicesByConnection :one
SELECT count(*) FROM services WHERE connection_id = $1;
