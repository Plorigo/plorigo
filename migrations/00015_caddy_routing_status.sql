-- +goose Up
-- Caddy routing is now an explicit deployment phase between starting the container and
-- marking it running. A deployment only reaches running after the agent validates and
-- reloads the Plorigo-managed Caddy route.
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'assigned', 'cloning', 'building', 'pulling', 'starting', 'routing', 'running', 'failed', 'superseded'));

-- +goose Down
-- Collapse an in-flight routing phase back to starting before restoring the old status
-- vocabulary.
UPDATE deployments SET status = 'starting' WHERE status = 'routing';
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'assigned', 'cloning', 'building', 'pulling', 'starting', 'running', 'failed', 'superseded'));
