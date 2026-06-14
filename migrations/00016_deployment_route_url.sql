-- +goose Up
-- Add route_url to store the real deployment URL (e.g. http://{env-id}.localhost:8083)
-- computed by the agent and surfaced in the dashboard.
ALTER TABLE deployments ADD COLUMN route_url text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE deployments DROP COLUMN IF EXISTS route_url;
