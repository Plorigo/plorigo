-- +goose Up
-- A teardown_job removes a PREVIEW deployment on demand. The agent on the preview's server stops
-- and removes its container (found by the plorigo.service={route_key} label) and reconciles Caddy
-- so the route drops (Caddy is rebuilt from Docker truth, so removing the container removes the
-- route). It mirrors restore_jobs: the control plane records it 'queued', the agent on that server
-- claims it atomically (see ClaimNextTeardownForServer) and reports progress. route_key identifies
-- the preview's container-replacement group + Caddy route; deployment_id links it to the preview
-- the user removed; workspace/project/environment are denormalized from the service (immutable) so
-- authorization and the dashboard need no joins. See docs/architecture/deployment-engine.md.
CREATE TABLE teardown_jobs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id  uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    service_id     uuid NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    route_key      text NOT NULL, -- the preview's plorigo.service label + Caddy route key
    environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    server_id      uuid NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    status         text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'assigned', 'stopping', 'removing', 'succeeded', 'failed')),
    message        text NOT NULL DEFAULT '',
    error          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_teardown_jobs_service ON teardown_jobs (service_id, created_at DESC);
-- The agent's claim filters on (server_id, status); index it.
CREATE INDEX idx_teardown_jobs_claim ON teardown_jobs (server_id, status);

-- A torn-down preview's deployment rows move to a terminal 'torndown' status so the dashboard
-- stops showing the preview as running once its container and route are gone. Extend the
-- deployments status CHECK to allow it (cf. 00023_deployment_healthcheck_status.sql).
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'assigned', 'cloning', 'building', 'pulling', 'starting', 'healthcheck', 'routing', 'running', 'failed', 'superseded', 'torndown'));

-- +goose Down
-- Fold any torn-down rows back to 'superseded' before tightening the constraint (cf. 00023).
UPDATE deployments SET status = 'superseded' WHERE status = 'torndown';
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'assigned', 'cloning', 'building', 'pulling', 'starting', 'healthcheck', 'routing', 'running', 'failed', 'superseded'));
DROP TABLE IF EXISTS teardown_jobs;
