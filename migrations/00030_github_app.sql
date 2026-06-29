-- +goose Up
-- GitHub App connections. A workspace can connect a GitHub App INSTALLATION (for reading private
-- repos/PRs with short-lived per-installation tokens) alongside or instead of an OAuth connection.
-- An App connection carries an installation_id and NO stored token — Plorigo mints a per-installation
-- access token on demand from the App's private key (held only in the control plane). An OAuth
-- connection still carries a sealed token and no installation_id. provider distinguishes them; the
-- UNIQUE (workspace_id, provider) from 00011 lets a workspace hold one of each. See
-- docs/architecture/security.md.
ALTER TABLE source_connections
    DROP CONSTRAINT source_connections_provider_check,
    ADD CONSTRAINT source_connections_provider_check CHECK (provider IN ('github', 'github_app')),
    ALTER COLUMN access_token_ciphertext DROP NOT NULL,
    ADD COLUMN installation_id text;

-- Enforce the per-provider credential shape: an OAuth row has a token and no installation; an App
-- row has an installation and no token.
ALTER TABLE source_connections ADD CONSTRAINT source_connections_credential_ck CHECK (
    (provider = 'github' AND access_token_ciphertext IS NOT NULL AND installation_id IS NULL)
    OR (provider = 'github_app' AND installation_id IS NOT NULL)
);

-- +goose Down
-- App connections only exist because of this migration and can't satisfy the restored NOT NULL, so
-- reversing it removes them (cf. 00012_public_sources.sql).
DELETE FROM source_connections WHERE provider = 'github_app';
ALTER TABLE source_connections
    DROP CONSTRAINT source_connections_credential_ck,
    DROP COLUMN installation_id,
    ALTER COLUMN access_token_ciphertext SET NOT NULL,
    DROP CONSTRAINT source_connections_provider_check,
    ADD CONSTRAINT source_connections_provider_check CHECK (provider IN ('github'));
