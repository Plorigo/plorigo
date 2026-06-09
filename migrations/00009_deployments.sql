-- +goose Up
-- A deployment is one attempt to run a container image in an environment on a
-- connected server (see docs/architecture/deployment-engine.md). The control plane
-- records it as 'queued'; the server's agent claims it (atomically, see
-- ClaimNextDeploymentForServer), pulls the image, runs the container, and reports
-- progress back. workspace_id and project_id are denormalized from the environment
-- (both immutable) so authorization, scoping, and the dashboard's project/workspace
-- views need no joins.
CREATE TABLE deployments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    server_id      uuid NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    image_ref      text NOT NULL,
    container_port integer NOT NULL,
    host_port      integer NOT NULL DEFAULT 0,
    container_id   text NOT NULL DEFAULT '',
    -- Defense-in-depth: status is validated in Go, but a CHECK guarantees no
    -- out-of-vocabulary status can ever be written (cf. 00004_environments.sql).
    status         text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'assigned', 'pulling', 'starting', 'running', 'failed', 'superseded')),
    message        text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_deployments_environment ON deployments (environment_id, created_at DESC);
CREATE INDEX idx_deployments_project ON deployments (project_id, created_at DESC);
CREATE INDEX idx_deployments_workspace ON deployments (workspace_id, created_at DESC);
-- The agent's claim filters on (server_id, status); index it.
CREATE INDEX idx_deployments_claim ON deployments (server_id, status);

-- A deployment_event is one entry in a deployment's timeline: a status transition
-- (kind='status') or a runtime log line (kind='log'). seq is a global monotonic
-- counter; within a deployment it strictly increases, so the dashboard fetches new
-- entries with `seq > after_seq`.
CREATE TABLE deployment_events (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id uuid NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    seq           bigserial NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('status', 'log')),
    status        text NOT NULL DEFAULT '',
    message       text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_deployment_events_deployment ON deployment_events (deployment_id, seq);

-- +goose Down
DROP TABLE IF EXISTS deployment_events;
DROP TABLE IF EXISTS deployments;
