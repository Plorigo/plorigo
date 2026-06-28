-- +goose Up
-- A restore_job restores a succeeded backup back into its database service — the proof that a
-- backup is usable. It mirrors the backups table: the control plane records it 'queued', the
-- agent on the database's server claims it (atomically, see ClaimNextRestoreForServer), reads the
-- backup artifact from the server's disk and pipes it into the database (psql), and reports
-- progress. artifact_uri is copied from the source backup at create time so the claim is
-- self-contained. workspace/project/environment are denormalized from the service (immutable) so
-- authorization and the dashboard need no joins. See docs/architecture/backups.md.
CREATE TABLE restore_jobs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    backup_id      uuid NOT NULL REFERENCES backups (id) ON DELETE CASCADE,
    service_id     uuid NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    server_id      uuid NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    artifact_uri   text NOT NULL DEFAULT '', -- the source dump on the server, copied from the backup
    status         text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'assigned', 'restoring', 'verifying', 'succeeded', 'failed')),
    message        text NOT NULL DEFAULT '',
    error          text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_restore_jobs_service ON restore_jobs (service_id, created_at DESC);
-- The agent's claim filters on (server_id, status); index it.
CREATE INDEX idx_restore_jobs_claim ON restore_jobs (server_id, status);

-- +goose Down
DROP TABLE IF EXISTS restore_jobs;
