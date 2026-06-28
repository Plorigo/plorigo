-- GetServerWorkspace resolves a server's owning workspace so agent actions authorize
-- against it WITHOUT importing the servers module (see modules.md Rule 4: an ancestor
-- lookup from postgres.go is exactly what Rule 2 permits).
-- name: GetServerWorkspace :one
SELECT workspace_id FROM servers WHERE id = $1;

-- name: CreateAgentRegistrationToken :one
INSERT INTO agent_registration_tokens (server_id, workspace_id, token_hash, created_by, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, server_id, workspace_id, created_at, expires_at, consumed_at;

-- ConsumeAgentRegistrationToken atomically validates and consumes a one-time token by
-- its hash. The WHERE clause enforces single use and expiry, so a replay returns no rows.
-- name: ConsumeAgentRegistrationToken :one
UPDATE agent_registration_tokens
SET consumed_at = now()
WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now()
RETURNING id, server_id, workspace_id;

-- UpsertAgent registers (or re-registers) the single agent for a server. A reinstall
-- rotates the public key and credential and clears the last heartbeat and reported health
-- facts, so a re-connected server starts from "unknown" until its first beat.
-- name: UpsertAgent :one
INSERT INTO agents (server_id, workspace_id, public_key, credential_hash, agent_version)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (server_id) DO UPDATE
    SET public_key       = EXCLUDED.public_key,
        credential_hash  = EXCLUDED.credential_hash,
        agent_version    = EXCLUDED.agent_version,
        last_seen_at     = NULL,
        docker_available = NULL,
        docker_version   = '',
        os               = '',
        arch             = ''
RETURNING id, server_id, workspace_id, agent_version, docker_available, docker_version, os, arch, last_seen_at, created_at;

-- HeartbeatAgent validates the durable credential by its hash AND records liveness plus
-- the latest compatibility facts in one statement (see UseAPIToken in auth.sql). Returns
-- the agent when the hash matches.
-- name: HeartbeatAgent :one
UPDATE agents
SET last_seen_at        = now(),
    agent_version       = $2,
    docker_available    = $3,
    docker_version      = $4,
    os                  = $5,
    arch                = $6,
    caddy_available     = $7,
    caddy_running       = $8,
    caddy_version       = $9,
    disk_total_bytes    = $10,
    disk_free_bytes     = $11,
    mem_total_bytes     = $12,
    mem_available_bytes = $13,
    cpu_count           = $14
WHERE credential_hash = $1
RETURNING id, server_id, workspace_id, agent_version, docker_available, docker_version, os, arch, caddy_available, caddy_running, caddy_version, disk_total_bytes, disk_free_bytes, mem_total_bytes, mem_available_bytes, cpu_count, last_seen_at, created_at;

-- name: ListAgentsByWorkspace :many
SELECT id, server_id, workspace_id, agent_version, docker_available, docker_version, os, arch, caddy_available, caddy_running, caddy_version, disk_total_bytes, disk_free_bytes, mem_total_bytes, mem_available_bytes, cpu_count, last_seen_at, created_at
FROM agents
WHERE workspace_id = $1
ORDER BY created_at DESC;
