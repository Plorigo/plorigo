-- +goose Up
-- Custom domains are service-scoped: a public service can keep its generated route_url and
-- attach any number of user-owned hostnames. Verification and route-sync state are stored
-- here so domain failures are visible in the dashboard before HTTPS lands.
CREATE TABLE domains (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id      uuid NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    environment_id  uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    project_id      uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    workspace_id    uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    hostname        text NOT NULL,
    status          text NOT NULL DEFAULT 'pending_dns'
        CHECK (status IN ('blocked', 'pending_dns', 'verified', 'active', 'failed')),
    status_message  text NOT NULL DEFAULT '',
    last_checked_at timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, hostname)
);

CREATE INDEX idx_domains_service ON domains (service_id, created_at DESC);
CREATE INDEX idx_domains_workspace ON domains (workspace_id, created_at DESC);
CREATE INDEX idx_domains_status ON domains (status);

-- +goose Down
DROP TABLE IF EXISTS domains;
