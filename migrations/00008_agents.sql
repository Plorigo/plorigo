-- +goose Up
-- Agent identity & registration (see docs/architecture/agent.md). The agent generates
-- an ed25519 keypair on install; the control plane stores only the PUBLIC key.
-- Registration uses a one-time token (stored hashed, like every token in 00002_auth)
-- exchanged for a durable agent credential, also stored only as its sha256 hash. The
-- agent connects OUTBOUND — the control plane never holds an SSH key to the server.

-- One agent per connected server.
CREATE TABLE agents (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id       uuid NOT NULL UNIQUE REFERENCES servers (id) ON DELETE CASCADE,
    workspace_id    uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    public_key      bytea NOT NULL,        -- ed25519 public key (32 bytes)
    credential_hash bytea NOT NULL UNIQUE, -- sha256 of the durable agent credential
    agent_version   text NOT NULL DEFAULT '',
    last_seen_at    timestamptz,           -- NULL until the first heartbeat
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_agents_workspace_id ON agents (workspace_id);

-- One-time tokens that authorize a single agent registration onto a specific server.
-- The raw token lives only in the install command shown once in the dashboard.
CREATE TABLE agent_registration_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id    uuid NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL UNIQUE,
    created_by   uuid NOT NULL REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz
);

CREATE INDEX idx_agent_registration_tokens_server ON agent_registration_tokens (server_id);

-- +goose Down
DROP TABLE IF EXISTS agent_registration_tokens;
DROP TABLE IF EXISTS agents;
