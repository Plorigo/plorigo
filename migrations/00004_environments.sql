-- +goose Up
-- An environment is a deployment target within a project (preview / staging /
-- production / custom) — the third leg of the workspace -> project -> environment
-- model (see docs/architecture/data-and-api.md). Authorization is workspace-scoped;
-- the owning workspace is resolved through the parent project.
CREATE TABLE environments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text NOT NULL,
    slug       text NOT NULL,
    -- Defense-in-depth: type is validated in Go, but a CHECK guarantees no
    -- out-of-vocabulary type can ever be written (cf. 00003_role_check.sql).
    type       text NOT NULL DEFAULT 'preview'
        CHECK (type IN ('preview', 'staging', 'production', 'custom')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, slug)
);

CREATE INDEX idx_environments_project_id ON environments (project_id);

-- +goose Down
DROP TABLE IF EXISTS environments;
