-- Queries for the backups module (internal/backups). The agent claim mirrors the deployment
-- claim (single-statement FOR UPDATE SKIP LOCKED). Credential resolution and config reads reuse
-- the existing GetAgentServerByCredential / ListConfigForService queries.

-- name: CreateBackup :one
INSERT INTO backups (service_id, environment_id, project_id, workspace_id, server_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetBackup :one
SELECT * FROM backups WHERE id = $1;

-- name: ListBackupsByService :many
SELECT * FROM backups WHERE service_id = $1 ORDER BY created_at DESC;

-- name: CountBackupsForService :one
SELECT count(*) FROM backups WHERE service_id = $1 AND status = 'succeeded';

-- ClaimNextBackupForServer atomically claims the oldest queued backup for a server, flipping it
-- to 'assigned' so concurrent polls never grab the same one (cf. ClaimNextDeploymentForServer).
-- name: ClaimNextBackupForServer :one
UPDATE backups
SET status = 'assigned', updated_at = now()
WHERE id = (
    SELECT b.id FROM backups b
    WHERE b.server_id = $1 AND b.status = 'queued'
    ORDER BY b.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- UpdateBackupStatus records a status transition. artifact_uri, size_bytes, checksum, message,
-- and error are only known at certain points, so a zero/empty value never clobbers a set one.
-- name: UpdateBackupStatus :one
UPDATE backups
SET status = sqlc.arg(status),
    message = CASE WHEN sqlc.arg(message)::text <> '' THEN sqlc.arg(message)::text ELSE message END,
    error = CASE WHEN sqlc.arg(error)::text <> '' THEN sqlc.arg(error)::text ELSE error END,
    artifact_uri = CASE WHEN sqlc.arg(artifact_uri)::text <> '' THEN sqlc.arg(artifact_uri)::text ELSE artifact_uri END,
    size_bytes = CASE WHEN sqlc.arg(size_bytes)::bigint > 0 THEN sqlc.arg(size_bytes)::bigint ELSE size_bytes END,
    checksum = CASE WHEN sqlc.arg(checksum)::text <> '' THEN sqlc.arg(checksum)::text ELSE checksum END,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- GetBackupServiceTarget resolves the database service a backup targets: its denormalized
-- workspace/project/environment, its name, and its source/template kind (so the module can
-- confirm it is a managed Postgres template before backing it up).
-- name: GetBackupServiceTarget :one
SELECT id, name, environment_id, project_id, workspace_id, source_kind, template_id
FROM services WHERE id = $1;

-- GetLatestRunningServerForService returns the server the service's current (running) container
-- is on — the backup must run on that server's agent.
-- name: GetLatestRunningServerForService :one
SELECT server_id FROM deployments
WHERE service_id = $1 AND status = 'running'
ORDER BY created_at DESC
LIMIT 1;

-- --- restore_jobs ---

-- name: CreateRestoreJob :one
INSERT INTO restore_jobs (backup_id, service_id, environment_id, project_id, workspace_id, server_id, artifact_uri)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetRestoreJob :one
SELECT * FROM restore_jobs WHERE id = $1;

-- name: ListRestoreJobsByService :many
SELECT * FROM restore_jobs WHERE service_id = $1 ORDER BY created_at DESC;

-- ClaimNextRestoreForServer atomically claims the oldest queued restore for a server (cf.
-- ClaimNextBackupForServer).
-- name: ClaimNextRestoreForServer :one
UPDATE restore_jobs
SET status = 'assigned', updated_at = now()
WHERE id = (
    SELECT r.id FROM restore_jobs r
    WHERE r.server_id = $1 AND r.status = 'queued'
    ORDER BY r.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: UpdateRestoreStatus :one
UPDATE restore_jobs
SET status = sqlc.arg(status),
    message = CASE WHEN sqlc.arg(message)::text <> '' THEN sqlc.arg(message)::text ELSE message END,
    error = CASE WHEN sqlc.arg(error)::text <> '' THEN sqlc.arg(error)::text ELSE error END,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;
