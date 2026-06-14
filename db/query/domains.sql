-- Custom domains are hostnames attached to a service. A service can have many domains,
-- but a hostname is unique within a workspace so traffic can never ambiguously route to
-- two services.

-- name: CreateDomain :one
INSERT INTO domains (
    service_id, environment_id, project_id, workspace_id, hostname, status, status_message
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetDomain :one
SELECT * FROM domains WHERE id = $1;

-- name: ListDomainsByService :many
SELECT * FROM domains WHERE service_id = $1 ORDER BY created_at DESC;

-- name: ListDomainsByProject :many
SELECT * FROM domains WHERE project_id = $1 ORDER BY created_at DESC;

-- name: ListDomainsByWorkspace :many
SELECT * FROM domains WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: UpdateDomainVerification :one
UPDATE domains
SET status = $2,
    status_message = $3,
    last_checked_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkDomainsRouteSync :exec
UPDATE domains
SET status = $3,
    status_message = $4,
    updated_at = now()
WHERE service_id = $1
  AND hostname = ANY($2::text[])
  AND status IN ('verified', 'active', 'failed');

-- name: VerifiedDomainsForServices :many
SELECT service_id, hostname
FROM domains
WHERE service_id = ANY($1::uuid[])
  AND status IN ('verified', 'active')
ORDER BY service_id, hostname;

-- name: DeleteDomain :one
DELETE FROM domains WHERE id = $1 RETURNING id;

-- GetDomainServiceForCreate resolves the target service and its routing prerequisites.
-- Domains are only useful for public services with a generated route_url, but we still
-- persist blocked rows so the dashboard can show the next action.
-- name: GetDomainServiceForCreate :one
SELECT id, environment_id, project_id, workspace_id, visibility, route_url
FROM services
WHERE id = $1;
