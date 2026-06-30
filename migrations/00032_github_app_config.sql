-- +goose Up
-- Server-wide GitHub App credentials, created automatically via GitHub's App-manifest flow (or set
-- via env vars, which take precedence). A single row (singleton) holds the instance's App. The
-- private key, webhook secret, and OAuth client secret are sealed at rest (AES-256-GCM, sealed by
-- the control plane with APP_MASTER_KEY) and WRITE-ONLY — never returned through any RPC, never
-- logged, never sent to the agent. This mirrors `secrets` and `ssh_management_keys`. The app id,
-- slug, and client id are non-secret metadata. See docs/architecture/sources.md and security.md.
CREATE TABLE github_app_config (
    -- singleton is always true; as the PRIMARY KEY with a CHECK it allows at most ONE row, so the
    -- write is an upsert ON CONFLICT (singleton).
    singleton             boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    app_id                text NOT NULL,
    app_slug              text NOT NULL,
    client_id             text NOT NULL DEFAULT '',
    -- nonce||ciphertext of the App private key PEM. Write-only; a generous CHECK backstop.
    sealed_private_key    bytea NOT NULL CHECK (octet_length(sealed_private_key) <= 16384),
    -- nonce||ciphertext of the webhook secret. Write-only.
    sealed_webhook_secret bytea NOT NULL CHECK (octet_length(sealed_webhook_secret) <= 1024),
    -- nonce||ciphertext of the App's OAuth client secret. Write-only; may be empty.
    sealed_client_secret  bytea NOT NULL DEFAULT '\x',
    -- The user who registered the App. SET NULL on user delete; the audit log is the actor trail.
    created_by            uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS github_app_config;
