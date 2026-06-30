-- +goose Up
-- Multiple integrations per workspace, across providers. Until now source_connections allowed only
-- ONE row per (workspace, provider) — one GitHub OAuth + one GitHub App per workspace. This drops
-- that limit so a workspace can connect many accounts / App installations and (later) many providers,
-- and generalizes the GitHub-specific columns. Each row is one "integration"; a service references
-- the specific connection it builds from (services.connection_id). The server-wide GitHub App itself
-- stays a singleton (github_app_config). See docs/architecture/sources.md.

-- provider + kind replace the conflated provider values: provider is the system (github; later
-- gitlab), kind is how it's reached (an OAuth token vs an App installation).
ALTER TABLE source_connections ADD COLUMN kind text;
UPDATE source_connections SET kind = 'oauth' WHERE provider = 'github';
UPDATE source_connections SET kind = 'app', provider = 'github' WHERE provider = 'github_app';
ALTER TABLE source_connections ALTER COLUMN kind SET NOT NULL;

-- Generalize the GitHub-specific column names to provider-neutral ones.
ALTER TABLE source_connections RENAME COLUMN github_login TO account_login;
ALTER TABLE source_connections RENAME COLUMN github_user_id TO account_id;

-- Replace the one-per-provider UNIQUE and the old provider/credential CHECKs.
ALTER TABLE source_connections DROP CONSTRAINT IF EXISTS source_connections_workspace_id_provider_key;
ALTER TABLE source_connections DROP CONSTRAINT source_connections_provider_check;
ALTER TABLE source_connections DROP CONSTRAINT source_connections_credential_ck;
ALTER TABLE source_connections
    ADD CONSTRAINT source_connections_provider_check CHECK (provider IN ('github', 'gitlab')),
    ADD CONSTRAINT source_connections_kind_check CHECK (kind IN ('oauth', 'app')),
    -- Credential shape keyed by kind: an oauth row has a sealed token and no installation; an app
    -- row has an installation and no token.
    ADD CONSTRAINT source_connections_credential_ck CHECK (
        (kind = 'oauth' AND access_token_ciphertext IS NOT NULL AND installation_id IS NULL)
        OR (kind = 'app' AND installation_id IS NOT NULL AND access_token_ciphertext IS NULL)
    );

-- Dedupe identities in place of the dropped UNIQUE:
--   * an App installation id is globally unique (one workspace owns a given installation);
--   * reconnecting the same account (same provider + kind) updates the existing row, not a duplicate.
CREATE UNIQUE INDEX source_connections_installation_key
    ON source_connections (provider, installation_id) WHERE installation_id IS NOT NULL;
CREATE UNIQUE INDEX source_connections_account_key
    ON source_connections (workspace_id, provider, kind, account_id) WHERE account_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS source_connections_account_key;
DROP INDEX IF EXISTS source_connections_installation_key;
ALTER TABLE source_connections
    DROP CONSTRAINT source_connections_credential_ck,
    DROP CONSTRAINT source_connections_kind_check,
    DROP CONSTRAINT source_connections_provider_check;
-- Collapse back to the old provider values (app rows become provider='github_app') and column names.
UPDATE source_connections SET provider = 'github_app' WHERE kind = 'app';
ALTER TABLE source_connections RENAME COLUMN account_id TO github_user_id;
ALTER TABLE source_connections RENAME COLUMN account_login TO github_login;
ALTER TABLE source_connections DROP COLUMN kind;
ALTER TABLE source_connections
    ADD CONSTRAINT source_connections_provider_check CHECK (provider IN ('github', 'github_app')),
    ADD CONSTRAINT source_connections_credential_ck CHECK (
        (provider = 'github' AND access_token_ciphertext IS NOT NULL AND installation_id IS NULL)
        OR (provider = 'github_app' AND installation_id IS NOT NULL)
    ),
    ADD CONSTRAINT source_connections_workspace_id_provider_key UNIQUE (workspace_id, provider);
