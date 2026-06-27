-- +goose Up
-- Dashboard-managed server setup: an auditable, asynchronous run that SSHes into a fresh
-- Ubuntu VPS, installs prerequisites via the shared installer, provisions the non-root
-- `plorigo` management user + sealed key (see ssh_management_keys / internal/serversetup),
-- and starts the outbound agent. The raw bootstrap credential the user supplies is NEVER
-- stored — it lives in memory for the active attempt only. See
-- docs/architecture/server-management.md.

-- TOFU host-key pin: captured on the first connection, enforced on every subsequent one.
ALTER TABLE servers ADD COLUMN host_key_fingerprint text NOT NULL DEFAULT '';

-- One setup run is one attempt to bootstrap a server. Retries create a new run; the run
-- row carries the overall lifecycle and a plain-English failure reason.
CREATE TABLE server_setup_runs (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id      uuid NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    -- Denormalized from the server (immutable) so status reads authorize without a join,
    -- mirroring deployments.
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    status         text NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    -- Plain-English reason for a failed run (apt lock, unsupported OS, …). Never a secret.
    failure_reason text NOT NULL DEFAULT '',
    started_by     uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz
);

CREATE INDEX idx_server_setup_runs_server ON server_setup_runs (server_id, created_at DESC);

-- Ordered, append-only status/log lines for a run, polled by the dashboard via a monotonic
-- seq (mirrors deployment_events). Messages are redacted, plain-English output only — never
-- a raw credential, private key, or registration token.
CREATE TABLE server_setup_events (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    setup_run_id  uuid NOT NULL REFERENCES server_setup_runs (id) ON DELETE CASCADE,
    seq           bigserial NOT NULL,
    -- The bootstrap step this line belongs to (detect_os, install_prereqs, provision_user, …).
    step          text NOT NULL DEFAULT '',
    kind          text NOT NULL CHECK (kind IN ('status', 'log')),
    -- For status events: started | ok | failed | skipped.
    status        text NOT NULL DEFAULT '',
    message       text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_server_setup_events_run ON server_setup_events (setup_run_id, seq);

-- +goose Down
DROP TABLE IF EXISTS server_setup_events;
DROP TABLE IF EXISTS server_setup_runs;
ALTER TABLE servers DROP COLUMN IF EXISTS host_key_fingerprint;
