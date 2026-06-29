-- +goose Up
-- Preview deployments: a service may have a production deployment AND per-branch / per-PR
-- preview deployments running at the same time. `kind` distinguishes them. `route_key`
-- isolates a preview from production (and from other previews) across the three places the
-- agent and control plane key a deployment by service today: the Caddy route host, the
-- container-replacement group, and the DB "supersede previous running" scope. Production
-- keeps route_key = its service id, so its URL and supersede semantics are unchanged.
-- pr_number / pr_url link a PR preview back to its GitHub pull request (0 / '' for a plain
-- branch preview). See docs/architecture/deployment-engine.md.
ALTER TABLE deployments
    ADD COLUMN kind text NOT NULL DEFAULT 'production'
        CHECK (kind IN ('production', 'preview')),
    ADD COLUMN route_key text NOT NULL DEFAULT '',
    ADD COLUMN pr_number integer NOT NULL DEFAULT 0,
    ADD COLUMN pr_url text NOT NULL DEFAULT '';

-- Existing rows are all production: key each by its own service id so SupersedePreviousRunning
-- (now keyed on route_key) preserves the exact per-service behavior it had before. New
-- production rows set route_key = service_id at insert time; previews set a derived key.
UPDATE deployments SET route_key = service_id::text WHERE route_key = '';

-- +goose Down
ALTER TABLE deployments
    DROP COLUMN kind,
    DROP COLUMN route_key,
    DROP COLUMN pr_number,
    DROP COLUMN pr_url;
