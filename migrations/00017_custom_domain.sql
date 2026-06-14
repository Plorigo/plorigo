-- +goose Up
-- Custom domain for a deployment: a user-supplied domain that the agent adds as an
-- additional Caddy route alongside the auto-generated {env-id}.{base-domain}. Stored
-- per-deployment in this prototype; production will likely move it to the environment
-- so it persists across redeploys.
ALTER TABLE deployments ADD COLUMN custom_domain text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments DROP COLUMN IF EXISTS custom_domain;
