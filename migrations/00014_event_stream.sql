-- +goose Up
-- A deployment_event of kind='log' belongs to one of two streams: the agent's own
-- BUILD output (clone/build/pull/start progress) or the container's RUNTIME stdout/stderr.
-- Until now both were stored undifferentiated as kind='log', so the dashboard could not
-- show build logs and runtime logs separately (PLO-13). Add an orthogonal discriminator:
-- kind stays 'status'|'log'; status events keep stream='' and log events are tagged
-- 'build' or 'runtime'. Existing rows default to '' (legacy/unknown) and the dashboard
-- buckets those with build logs. See docs/architecture/deployment-engine.md.
ALTER TABLE deployment_events
    ADD COLUMN stream text NOT NULL DEFAULT ''
        CHECK (stream IN ('', 'build', 'runtime'));

-- +goose Down
ALTER TABLE deployment_events DROP COLUMN stream;
