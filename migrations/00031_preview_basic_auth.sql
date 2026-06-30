-- +goose Up
-- Optional basic-auth protection for a PREVIEW deployment's public URL: a not-yet-public preview can
-- be made to require a username + password. Only the bcrypt HASH is stored here (and later rendered
-- into the preview's Caddy route by the agent) — the plaintext password never touches the database
-- or the agent. Empty = unprotected (the default); production deployments never set these. See
-- docs/architecture/deployment-engine.md and security.md.
ALTER TABLE deployments
    ADD COLUMN preview_auth_user text NOT NULL DEFAULT '',
    ADD COLUMN preview_auth_hash text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments
    DROP COLUMN preview_auth_user,
    DROP COLUMN preview_auth_hash;
