-- +goose Up
-- The container health check is now its own explicit deployment phase between starting the
-- container and routing traffic to it. Surfacing it distinctly lets the timeline attribute a
-- "did not become healthy" failure to the health check rather than to "start container".
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'assigned', 'cloning', 'building', 'pulling', 'starting', 'healthcheck', 'routing', 'running', 'failed', 'superseded'));

-- +goose Down
-- Collapse an in-flight health-check phase back to starting before restoring the old
-- status vocabulary.
UPDATE deployments SET status = 'starting' WHERE status = 'healthcheck';
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'assigned', 'cloning', 'building', 'pulling', 'starting', 'routing', 'running', 'failed', 'superseded'));
