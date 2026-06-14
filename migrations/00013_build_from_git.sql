-- +goose Up
-- Build-from-Git: a deployment can now run an image BUILT FROM SOURCE on the server, not
-- just a pre-built image. A 'git' deployment carries the repo clone URL + ref; the agent
-- clones it, builds the Dockerfile with BuildKit, and runs the resulting image, reporting
-- the exact commit and the local image tag it produced. This slice builds PUBLIC
-- repositories only — no credential ever leaves the control plane; private/OAuth repos
-- arrive later with the GitHub App installation-token path. See
-- docs/architecture/deployment-engine.md, agent.md, and security.md.
ALTER TABLE deployments
    -- 'image' (today's pre-built path) or 'git' (clone + build from source).
    ADD COLUMN source_kind text NOT NULL DEFAULT 'image'
        CHECK (source_kind IN ('image', 'git')),
    -- How the source repo is reached, mirroring project_sources.access ('public' this
    -- slice). '' for image deployments.
    ADD COLUMN source_access text NOT NULL DEFAULT '',
    -- The git clone URL and the ref (branch/tag) to build, for git deployments.
    ADD COLUMN clone_url text NOT NULL DEFAULT '',
    ADD COLUMN git_ref text NOT NULL DEFAULT '',
    -- Filled by the agent after a successful clone/build: the exact commit built and the
    -- local image tag it produced. Empty until reported (cf. host_port / container_id).
    ADD COLUMN commit_sha text NOT NULL DEFAULT '',
    ADD COLUMN built_image_ref text NOT NULL DEFAULT '',
    -- A git deployment has no pre-built image at create time; default image_ref to '' so
    -- the column stays NOT NULL without the caller supplying one.
    ALTER COLUMN image_ref SET DEFAULT '';

-- Extend the status vocabulary with the two build phases the agent reports between
-- 'assigned' and 'starting'. Postgres can't alter a CHECK in place, so drop + re-add it.
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'assigned', 'cloning', 'building', 'pulling', 'starting', 'running', 'failed', 'superseded'));

-- +goose Down
ALTER TABLE deployments DROP CONSTRAINT deployments_status_check;
ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
    CHECK (status IN ('queued', 'assigned', 'pulling', 'starting', 'running', 'failed', 'superseded'));
ALTER TABLE deployments
    ALTER COLUMN image_ref DROP DEFAULT,
    DROP COLUMN built_image_ref,
    DROP COLUMN commit_sha,
    DROP COLUMN git_ref,
    DROP COLUMN clone_url,
    DROP COLUMN source_access,
    DROP COLUMN source_kind;
