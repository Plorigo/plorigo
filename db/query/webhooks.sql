-- Queries for the webhooks module (internal/webhooks). It maps a GitHub webhook's installation +
-- repo to the workspace's matching git services, reading source_connections + services as
-- sibling-table reads (modules.md Rule 2 permits this from a module's postgres.go).

-- name: GetWorkspaceByInstallation :one
-- Resolve a GitHub App installation id to its workspace, so a webhook delivery is scoped to the
-- workspace that connected the installation. ErrNoRows for an unknown/unconnected installation.
SELECT workspace_id FROM source_connections
WHERE provider = 'github_app' AND installation_id = $1;

-- name: ListServiceIDsForRepo :many
-- The git services in a workspace whose source is owner/repo (case-insensitive), so a PR event maps
-- to the service(s) to preview. A repo may back more than one service.
SELECT id FROM services
WHERE workspace_id = $1
  AND source_kind = 'git'
  AND lower(owner) = lower($2)
  AND lower(repo) = lower($3);
