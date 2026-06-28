-- +goose Up
-- A backup is one attempt to capture a managed Postgres database service's data with pg_dump,
-- run by the database's server agent and stored on the server (local disk for the MVP; an
-- S3-compatible destination is a later slice). The control plane records it 'queued'; the agent
-- on the database's server claims it (atomically, see ClaimNextBackupForServer), runs pg_dump
-- inside the container, and reports progress. workspace_id, project_id, and environment_id are
-- denormalized from the service (all immutable) so authorization, scoping, and the dashboard's
-- views need no joins — the same pattern as deployments. See docs/architecture/backups.md.
CREATE TABLE backups (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id     uuid NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    server_id      uuid NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    -- Where the artifact lives. 'local' = on the server's own disk (MVP); 's3' is a later slice.
    destination    text NOT NULL DEFAULT 'local',
    artifact_uri   text NOT NULL DEFAULT '',
    size_bytes     bigint NOT NULL DEFAULT 0,
    checksum       text NOT NULL DEFAULT '', -- sha256 of the artifact, for integrity
    -- Defense-in-depth: status is validated in Go, but a CHECK guarantees no out-of-vocabulary
    -- status can ever be written (cf. 00009_deployments.sql).
    status         text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'assigned', 'dumping', 'uploading', 'verifying', 'succeeded', 'failed')),
    message        text NOT NULL DEFAULT '',
    error          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_backups_service ON backups (service_id, created_at DESC);
CREATE INDEX idx_backups_workspace ON backups (workspace_id, created_at DESC);
-- The agent's claim filters on (server_id, status); index it.
CREATE INDEX idx_backups_claim ON backups (server_id, status);

-- +goose Down
DROP TABLE IF EXISTS backups;
