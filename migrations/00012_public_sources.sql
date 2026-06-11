-- +goose Up
-- Public repositories need no provider account, so a project source can exist with no
-- connection. Make connection_id nullable and record HOW the source is reached in an
-- explicit `access` discriminator rather than inferring "public" from a NULL connection
-- (that inference becomes ambiguous once the GitHub App lands: an app-backed source is
-- reached through a connection too, but differently). 'app' is allowed now so the App
-- slice adds rows without another constraint change. See docs/architecture/security.md.
ALTER TABLE project_sources
    ALTER COLUMN connection_id DROP NOT NULL,
    ADD COLUMN access text NOT NULL DEFAULT 'oauth'
        CHECK (access IN ('oauth', 'public', 'app')),
    -- A public source stands alone; an oauth/app source must point at a connection.
    ADD CONSTRAINT project_sources_access_connection_ck CHECK (
        (access = 'public' AND connection_id IS NULL)
        OR (access IN ('oauth', 'app') AND connection_id IS NOT NULL)
    );

-- +goose Down
-- Restoring NOT NULL requires that no public (connection-less) sources remain.
ALTER TABLE project_sources
    DROP CONSTRAINT project_sources_access_connection_ck,
    DROP COLUMN access,
    ALTER COLUMN connection_id SET NOT NULL;
