-- +goose Up
-- A non-secret, per-environment configuration key/value pair. Values are stored in
-- plaintext and are readable; encrypted secrets are a separate table/module (see
-- docs/architecture/data-and-api.md and security.md). Env vars are mutable, so they
-- carry updated_at. Authorization is workspace-scoped; the owning workspace is
-- resolved through environment -> project.
CREATE TABLE env_vars (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    -- Defense-in-depth: the key grammar and length bounds are validated in Go, but
    -- CHECKs guarantee no malformed key or oversized value can ever be written
    -- (cf. 00003_role_check.sql, 00004_environments.sql).
    key            text NOT NULL
        CHECK (key ~ '^[A-Z_][A-Z0-9_]*$' AND char_length(key) <= 128),
    value          text NOT NULL
        CHECK (char_length(value) <= 32768),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (environment_id, key)
);

CREATE INDEX idx_env_vars_environment_id ON env_vars (environment_id);

-- +goose Down
DROP TABLE IF EXISTS env_vars;
