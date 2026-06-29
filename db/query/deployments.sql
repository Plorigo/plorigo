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

-- rolled_back_from is NULL for a normal deploy and set to the restored deployment's id when
-- this row is a rollback (see RollbackDeployment). route_key = the service id for a production
-- deployment, so its Caddy route, container-replacement group, and supersede scope are keyed
-- by service exactly as before previews existed.
-- name: CreateDeployment :one
INSERT INTO deployments (service_id, route_key, environment_id, project_id, workspace_id, server_id, image_ref, container_port, rolled_back_from)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- CreateDeploymentFromGit records a queued deployment whose source is a repo+ref to clone
-- and build on the server (image_ref is filled in by the agent after the build). route_key =
-- the service id (a production deployment). See docs/architecture/deployment-engine.md.
-- name: CreateDeploymentFromGit :one
INSERT INTO deployments (
    service_id, route_key, environment_id, project_id, workspace_id, server_id,
    container_port, source_kind, source_access, clone_url, git_ref, rolled_back_from
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'git', $8, $9, $10, $11)
RETURNING *;

-- CreatePreviewDeployment records a queued PREVIEW deployment of a service: a build of a
-- branch or pull-request head ref, isolated from the service's production deployment by its
-- own route_key (which drives a distinct Caddy route, container-replacement group, and
-- supersede scope). pr_number / pr_url link it to a GitHub pull request (0 / '' for a plain
-- branch preview). Previews build PUBLIC git sources only in this slice.
-- name: CreatePreviewDeployment :one
INSERT INTO deployments (
    service_id, route_key, kind, environment_id, project_id, workspace_id, server_id,
    container_port, source_kind, source_access, clone_url, git_ref, pr_number, pr_url
)
VALUES ($1, $2, 'preview', $3, $4, $5, $6, $7, 'git', $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetDeployment :one
SELECT * FROM deployments WHERE id = $1;

-- name: ListDeploymentsByEnvironment :many
SELECT * FROM deployments WHERE environment_id = $1 ORDER BY created_at DESC;

-- name: ListDeploymentsByProject :many
SELECT * FROM deployments WHERE project_id = $1 ORDER BY created_at DESC;

-- name: ListDeploymentsByService :many
SELECT * FROM deployments WHERE service_id = $1 ORDER BY created_at DESC;

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

-- UpdateDeploymentStatus records a status transition. host_port, container_id, commit_sha,
-- built_image_ref, route_url, and message are only known at certain points in the flow,
-- so a zero/empty value never clobbers a set one. (The runtime-log tail loop re-reports
-- status='running' with an empty message to attach new log lines; a blank message must
-- not wipe the deployment's status line.)
-- name: UpdateDeploymentStatus :one
UPDATE deployments
SET status = sqlc.arg(status),
    message = CASE WHEN sqlc.arg(message)::text <> '' THEN sqlc.arg(message)::text ELSE message END,
    host_port = CASE WHEN sqlc.arg(host_port)::integer > 0 THEN sqlc.arg(host_port)::integer ELSE host_port END,
    container_id = CASE WHEN sqlc.arg(container_id)::text <> '' THEN sqlc.arg(container_id)::text ELSE container_id END,
    commit_sha = CASE WHEN sqlc.arg(commit_sha)::text <> '' THEN sqlc.arg(commit_sha)::text ELSE commit_sha END,
    built_image_ref = CASE WHEN sqlc.arg(built_image_ref)::text <> '' THEN sqlc.arg(built_image_ref)::text ELSE built_image_ref END,
    route_url = CASE WHEN sqlc.arg(route_url)::text <> '' THEN sqlc.arg(route_url)::text ELSE route_url END,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- SupersedePreviousRunning marks the prior running deployment with the same route_key on this
-- server as superseded once a newer one reaches 'running', so "current" is unambiguous. Keyed
-- by route_key (= the service id for a production deployment, a distinct key per preview) so a
-- preview never supersedes production (or another preview), and a sibling service in the same
-- environment is never superseded.
-- name: SupersedePreviousRunning :exec
UPDATE deployments
SET status = 'superseded', updated_at = now()
WHERE route_key = $1 AND server_id = $2 AND status = 'running' AND id <> $3;

-- name: AppendDeploymentEvent :one
INSERT INTO deployment_events (deployment_id, kind, status, message, stream)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListDeploymentEvents :many
SELECT * FROM deployment_events
WHERE deployment_id = $1 AND seq > $2
ORDER BY seq;
