-- name: UpsertGitHubAppConfig :one
-- Create-or-replace the instance's server-wide GitHub App credentials (singleton row). The conflict
-- is the success path (re-registering replaces the App), so callers never see AlreadyExists.
-- RETURNING yields metadata only — never the sealed key/secret, which are write-only.
INSERT INTO github_app_config (
    singleton, app_id, app_slug, client_id, sealed_private_key, sealed_webhook_secret, sealed_client_secret, created_by
)
VALUES (true, $1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (singleton) DO UPDATE SET
    app_id = EXCLUDED.app_id,
    app_slug = EXCLUDED.app_slug,
    client_id = EXCLUDED.client_id,
    sealed_private_key = EXCLUDED.sealed_private_key,
    sealed_webhook_secret = EXCLUDED.sealed_webhook_secret,
    sealed_client_secret = EXCLUDED.sealed_client_secret,
    created_by = EXCLUDED.created_by,
    updated_at = now()
RETURNING app_id, app_slug, client_id, created_at, updated_at;

-- name: GetGitHubAppConfig :one
-- Non-secret metadata for the configured App. Metadata only — never the sealed key/secret.
SELECT app_id, app_slug, client_id, created_at, updated_at
FROM github_app_config
WHERE singleton = true;

-- name: GetSealedGitHubAppConfig :one
-- The app id/slug + sealed private key, webhook secret, and client secret for in-process use only
-- (minting JWTs, verifying webhooks). INTERNAL — the sealed columns are never wired to a handler or
-- returned by any RPC.
SELECT app_id, app_slug, client_id, sealed_private_key, sealed_webhook_secret, sealed_client_secret
FROM github_app_config
WHERE singleton = true;

-- name: DeleteGitHubAppConfig :exec
-- Remove the stored App (e.g. before re-registering or to fall back to env config).
DELETE FROM github_app_config
WHERE singleton = true;
