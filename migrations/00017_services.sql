-- +goose Up
-- A service is a single deployable component of a project — its own source (a git repo, a
-- pre-built image, or a template), its own port, its own URL, its own env vars, and its own
-- deployment history. A project is a SYSTEM made of one or more services (web, api, worker,
-- db), each with a DIFFERENT source. A service lives in exactly one environment; the same app
-- running in production and preview is two services. workspace_id and project_id are
-- denormalized from the environment (both immutable) so authorization, scoping, and the
-- dashboard's project/workspace views need no joins (cf. deployments, 00009_deployments.sql).
-- The source is FOLDED onto this row, discriminated by source_kind, mirroring how deployments
-- folds image/git columns (00013_build_from_git.sql) — one row is one service with one
-- discriminated source. See docs/architecture/deployment-engine.md and data-and-api.md.
CREATE TABLE services (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    workspace_id   uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name           text NOT NULL,
    slug           text NOT NULL,
    -- 'image' (pre-built), 'git' (clone + build from source), or 'template' (a curated
    -- preset that resolves to an image). Mirrors deployments.source_kind, plus 'template'.
    source_kind    text NOT NULL DEFAULT 'image'
        CHECK (source_kind IN ('image', 'git', 'template')),
    -- image / template source:
    image_ref      text NOT NULL DEFAULT '',
    template_id    text NOT NULL DEFAULT '',
    -- git source (folded from project_sources). connection_id is NULL for a public repo;
    -- RESTRICT mirrors project_sources so disconnecting a provider still in use fails loudly.
    connection_id  uuid REFERENCES source_connections (id) ON DELETE RESTRICT,
    provider       text NOT NULL DEFAULT ''
        CHECK (provider IN ('', 'github')),
    owner          text NOT NULL DEFAULT '',
    repo           text NOT NULL DEFAULT '',
    full_name      text NOT NULL DEFAULT '',
    branch         text NOT NULL DEFAULT '',
    default_branch text NOT NULL DEFAULT '',
    is_private     boolean NOT NULL DEFAULT false,
    html_url       text NOT NULL DEFAULT '',
    -- How a git source is reached ('public' this slice; 'oauth'/'app' recorded but not yet
    -- buildable). '' for image/template. Mirrors project_sources.access.
    source_access  text NOT NULL DEFAULT ''
        CHECK (source_access IN ('', 'oauth', 'public', 'app')),
    -- The container's listening port. 0 = auto-detect from the Dockerfile EXPOSE (git builds).
    container_port integer NOT NULL DEFAULT 0,
    -- 'public' gets a Caddy route + public URL; 'private' is reachable only by sibling services
    -- in the same environment over the per-environment network, at http://{slug}:{port}.
    visibility     text NOT NULL DEFAULT 'public'
        CHECK (visibility IN ('public', 'private')),
    -- The current public URL, kept up to date from the latest running deployment. Empty for a
    -- private service (no public route) or before the first successful deploy.
    route_url      text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    -- Slug is unique within an environment; it also doubles as the service's DNS alias on the
    -- per-environment network, so it must be a valid DNS label (enforced in Go). The same name
    -- may exist in a different environment (that is a different service).
    UNIQUE (environment_id, slug),
    -- For a git source, mirror project_sources' access<->connection invariant (00012). Image
    -- and template sources have no connection.
    CONSTRAINT services_git_access_ck CHECK (
        source_kind <> 'git'
        OR (source_access = 'public' AND connection_id IS NULL)
        OR (source_access IN ('oauth', 'app') AND connection_id IS NOT NULL)
    )
);

CREATE INDEX idx_services_environment ON services (environment_id, created_at DESC);
CREATE INDEX idx_services_project ON services (project_id, created_at DESC);
CREATE INDEX idx_services_workspace ON services (workspace_id, created_at DESC);
CREATE INDEX idx_services_connection_id ON services (connection_id);

-- A deployment is now one deploy attempt OF A SERVICE. Added nullable for the backfill below,
-- then tightened to NOT NULL. environment_id stays (still denormalized for env-scoped views).
ALTER TABLE deployments ADD COLUMN service_id uuid REFERENCES services (id) ON DELETE CASCADE;
-- Env vars move from environment-scoped to service-scoped (each service is its own app).
ALTER TABLE env_vars ADD COLUMN service_id uuid REFERENCES services (id) ON DELETE CASCADE;

-- Backfill: give every existing environment that has deployments, env vars, or a connected
-- project source a single default service named 'web', folding the project's source (if any)
-- onto it. These set-based statements are natural no-ops on a fresh/empty database.
INSERT INTO services (
    environment_id, project_id, workspace_id, name, slug,
    source_kind, image_ref, connection_id, provider, owner, repo, full_name,
    branch, default_branch, is_private, html_url, source_access, container_port
)
SELECT
    e.id, e.project_id, p.workspace_id, 'web', 'web',
    CASE WHEN ps.id IS NOT NULL THEN 'git' ELSE 'image' END,
    COALESCE(latest.image_ref, ''),
    ps.connection_id, COALESCE(ps.provider, ''), COALESCE(ps.owner, ''), COALESCE(ps.repo, ''),
    COALESCE(ps.full_name, ''), COALESCE(ps.branch, ''), COALESCE(ps.default_branch, ''),
    COALESCE(ps.is_private, false), COALESCE(ps.html_url, ''), COALESCE(ps.access, ''), 0
FROM environments e
JOIN projects p ON p.id = e.project_id
LEFT JOIN project_sources ps ON ps.project_id = e.project_id
LEFT JOIN LATERAL (
    SELECT d.image_ref FROM deployments d
    WHERE d.environment_id = e.id AND d.image_ref <> ''
    ORDER BY d.created_at DESC
    LIMIT 1
) latest ON true
WHERE EXISTS (SELECT 1 FROM deployments d WHERE d.environment_id = e.id)
   OR EXISTS (SELECT 1 FROM env_vars ev WHERE ev.environment_id = e.id)
   OR ps.id IS NOT NULL;

-- Each environment has exactly one default service after the insert above, so attaching by
-- environment_id is unambiguous.
UPDATE deployments d
SET service_id = s.id
FROM services s
WHERE s.environment_id = d.environment_id AND d.service_id IS NULL;

UPDATE env_vars ev
SET service_id = s.id
FROM services s
WHERE s.environment_id = ev.environment_id AND ev.service_id IS NULL;

-- Every deployment and env var now belongs to a service.
ALTER TABLE deployments ALTER COLUMN service_id SET NOT NULL;
ALTER TABLE env_vars ALTER COLUMN service_id SET NOT NULL;

-- Env vars are service-scoped now. Dropping environment_id automatically drops the old
-- UNIQUE(environment_id, key) constraint and its index (Postgres cascades both with the
-- column); re-key uniqueness to the service.
ALTER TABLE env_vars DROP COLUMN environment_id;
ALTER TABLE env_vars ADD CONSTRAINT env_vars_service_id_key UNIQUE (service_id, key);
CREATE INDEX idx_env_vars_service_id ON env_vars (service_id);

-- Index the new service-scoped deployment list + per-service supersede.
CREATE INDEX idx_deployments_service ON deployments (service_id, created_at DESC);

-- project_sources is folded onto services now.
DROP TABLE project_sources;

-- +goose Down
-- Destructive: the Service entity owns source + env-var scoping, so reversing it drops services
-- and the per-service wiring. Folded sources and service-scoped env var rows are not recoverable
-- (they only exist because of this migration's reshaping).

-- Restore env_vars to environment scope (existing rows are dropped — their service association
-- can't be mapped back to an environment without services).
DELETE FROM env_vars;
ALTER TABLE env_vars DROP COLUMN service_id;
ALTER TABLE env_vars ADD COLUMN environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE;
ALTER TABLE env_vars ADD CONSTRAINT env_vars_environment_id_key UNIQUE (environment_id, key);
CREATE INDEX idx_env_vars_environment_id ON env_vars (environment_id);

-- Drop the service link from deployments (the deployment rows themselves survive).
DROP INDEX IF EXISTS idx_deployments_service;
ALTER TABLE deployments DROP COLUMN service_id;

-- Recreate project_sources (empty) as it stood after 00012_public_sources.
CREATE TABLE project_sources (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    connection_id  uuid REFERENCES source_connections (id) ON DELETE RESTRICT,
    provider       text NOT NULL DEFAULT 'github'
        CHECK (provider IN ('github')),
    owner          text NOT NULL,
    repo           text NOT NULL,
    full_name      text NOT NULL,
    branch         text NOT NULL,
    default_branch text NOT NULL DEFAULT '',
    is_private     boolean NOT NULL DEFAULT false,
    html_url       text NOT NULL DEFAULT '',
    access         text NOT NULL DEFAULT 'oauth'
        CHECK (access IN ('oauth', 'public', 'app')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT project_sources_access_connection_ck CHECK (
        (access = 'public' AND connection_id IS NULL)
        OR (access IN ('oauth', 'app') AND connection_id IS NOT NULL)
    ),
    UNIQUE (project_id)
);
CREATE INDEX idx_project_sources_connection_id ON project_sources (connection_id);

DROP TABLE services;
