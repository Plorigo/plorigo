-- GetEnvironmentWorkspaceAndProject resolves a deployment target's owning workspace
-- and project through the environment's parent project, so this module authorizes and
-- denormalizes both onto the deployment without importing environments/projects.
-- name: GetEnvironmentWorkspaceAndProject :one
SELECT p.workspace_id, p.id AS project_id
FROM environments e
JOIN projects p ON p.id = e.project_id
WHERE e.id = $1;

-- GetAgentServerByCredential resolves the agent and its server from a durable agent
-- credential hash, so the agent-facing Poll/Report RPCs authenticate the caller and
-- scope work to its own server (reading the agents table here is the cross-table read
-- modules.md Rule 2 permits from a module's postgres.go).
-- name: GetAgentServerByCredential :one
SELECT id, server_id FROM agents WHERE credential_hash = $1;

-- name: CreateDeployment :one
INSERT INTO deployments (environment_id, project_id, workspace_id, server_id, image_ref, container_port)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- CreateDeploymentFromGit records a queued deployment whose source is a repo+ref to clone
-- and build on the server (image_ref is filled in by the agent after the build). See
-- docs/architecture/deployment-engine.md.
-- name: CreateDeploymentFromGit :one
INSERT INTO deployments (
    environment_id, project_id, workspace_id, server_id,
    container_port, source_kind, source_access, clone_url, git_ref
)
VALUES ($1, $2, $3, $4, $5, 'git', $6, $7, $8)
RETURNING *;

-- GetProjectSourceForDeploy reads a project's connected repository so a git deployment can
-- resolve its clone URL and access kind, without importing the sources module (a sibling-
-- table read, which modules.md Rule 2 permits from a module's postgres.go).
-- name: GetProjectSourceForDeploy :one
SELECT owner, repo, branch, default_branch, access, html_url
FROM project_sources WHERE project_id = $1;

-- name: GetDeployment :one
SELECT * FROM deployments WHERE id = $1;

-- name: ListDeploymentsByEnvironment :many
SELECT * FROM deployments WHERE environment_id = $1 ORDER BY created_at DESC;

-- name: ListDeploymentsByProject :many
SELECT * FROM deployments WHERE project_id = $1 ORDER BY created_at DESC;

-- name: ListDeploymentsByWorkspace :many
SELECT * FROM deployments WHERE workspace_id = $1 ORDER BY created_at DESC;

-- ClaimNextDeploymentForServer atomically claims the oldest queued deployment for a
-- server, flipping it to 'assigned' so concurrent polls (or a restart mid-deploy)
-- never grab the same one. FOR UPDATE SKIP LOCKED + the single statement is the
-- single-statement-claim pattern (cf. ConsumeAgentRegistrationToken in agents.sql).
-- name: ClaimNextDeploymentForServer :one
UPDATE deployments
SET status = 'assigned', updated_at = now()
WHERE id = (
    SELECT d.id FROM deployments d
    WHERE d.server_id = $1 AND d.status = 'queued'
    ORDER BY d.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- UpdateDeploymentStatus records a status transition. host_port, container_id, commit_sha
-- and built_image_ref are only known later in the flow, so a zero/empty value never
-- clobbers a set one.
-- name: UpdateDeploymentStatus :one
UPDATE deployments
SET status = sqlc.arg(status),
    message = sqlc.arg(message),
    host_port = CASE WHEN sqlc.arg(host_port)::integer > 0 THEN sqlc.arg(host_port)::integer ELSE host_port END,
    container_id = CASE WHEN sqlc.arg(container_id)::text <> '' THEN sqlc.arg(container_id)::text ELSE container_id END,
    commit_sha = CASE WHEN sqlc.arg(commit_sha)::text <> '' THEN sqlc.arg(commit_sha)::text ELSE commit_sha END,
    built_image_ref = CASE WHEN sqlc.arg(built_image_ref)::text <> '' THEN sqlc.arg(built_image_ref)::text ELSE built_image_ref END,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- SupersedePreviousRunning marks the environment's prior running deployment on this
-- server as superseded once a newer one reaches 'running', so "current" is unambiguous.
-- name: SupersedePreviousRunning :exec
UPDATE deployments
SET status = 'superseded', updated_at = now()
WHERE environment_id = $1 AND server_id = $2 AND status = 'running' AND id <> $3;

-- name: AppendDeploymentEvent :one
INSERT INTO deployment_events (deployment_id, kind, status, message)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListDeploymentEvents :many
SELECT * FROM deployment_events
WHERE deployment_id = $1 AND seq > $2
ORDER BY seq;
