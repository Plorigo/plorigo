-- +goose Up
-- Unified configuration: one table for variables (plaintext, readable) and secrets
-- (AES-256-GCM ciphertext, sealed with APP_MASTER_KEY, WRITE-ONLY), at either service or
-- environment scope. Replaces the separate env_vars (service-scoped, plaintext) and
-- secrets (environment-scoped, encrypted) tables — the two axes (type, scope) are now
-- independent. At deploy a service receives its environment-shared entries merged with its
-- own service-level entries, the latter overriding on key collision. Authorization is
-- workspace-scoped, resolved through the service or the environment's project. See
-- docs/architecture/security.md and data-and-api.md.
CREATE TABLE config_entries (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type           text NOT NULL CHECK (type IN ('variable', 'secret')),
    scope          text NOT NULL CHECK (scope IN ('service', 'environment')),
    -- Exactly one scope target is set, matching scope (CHECK below). ON DELETE CASCADE so
    -- removing a service or environment removes its config.
    service_id     uuid REFERENCES services (id) ON DELETE CASCADE,
    environment_id uuid REFERENCES environments (id) ON DELETE CASCADE,
    -- Defense-in-depth: the key grammar/length and value/ciphertext bounds are validated in
    -- Go, but the CHECKs guarantee nothing malformed can ever be written.
    key            text NOT NULL
        CHECK (key ~ '^[A-Z_][A-Z0-9_]*$' AND char_length(key) <= 128),
    -- value is the plaintext for variables; ciphertext is the sealed bytes for secrets.
    -- Exactly one is set, matching type (CHECK below).
    value          text  CHECK (value IS NULL OR char_length(value) <= 32768),
    ciphertext     bytea CHECK (ciphertext IS NULL OR octet_length(ciphertext) <= 65536),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT config_scope_target CHECK (
        (scope = 'service'     AND service_id IS NOT NULL AND environment_id IS NULL) OR
        (scope = 'environment' AND environment_id IS NOT NULL AND service_id IS NULL)
    ),
    CONSTRAINT config_value_by_type CHECK (
        (type = 'variable' AND value IS NOT NULL AND ciphertext IS NULL) OR
        (type = 'secret'   AND ciphertext IS NOT NULL AND value IS NULL)
    )
);

-- A key is unique within a scope target — a variable and a secret cannot share a key in
-- one service (or one environment), mirroring the old per-scope UNIQUE constraints. The
-- SAME key may exist at both a service and its environment: that is the intended override.
CREATE UNIQUE INDEX config_entries_service_key ON config_entries (service_id, key)
    WHERE service_id IS NOT NULL;
CREATE UNIQUE INDEX config_entries_environment_key ON config_entries (environment_id, key)
    WHERE environment_id IS NOT NULL;

DROP TABLE IF EXISTS env_vars;
DROP TABLE IF EXISTS secrets;

-- +goose Down
-- Recreate the split tables in their pre-00019 shape (env_vars service-scoped per 00017,
-- secrets environment-scoped per 00007). Data in config_entries is not migrated back.
CREATE TABLE env_vars (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id   uuid NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    key          text NOT NULL
        CHECK (key ~ '^[A-Z_][A-Z0-9_]*$' AND char_length(key) <= 128),
    value        text NOT NULL CHECK (char_length(value) <= 32768),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (service_id, key)
);
CREATE INDEX idx_env_vars_service_id ON env_vars (service_id);

CREATE TABLE secrets (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    key            text NOT NULL
        CHECK (key ~ '^[A-Z_][A-Z0-9_]*$' AND char_length(key) <= 128),
    ciphertext     bytea NOT NULL CHECK (octet_length(ciphertext) <= 65536),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (environment_id, key)
);
CREATE INDEX idx_secrets_environment_id ON secrets (environment_id);

DROP TABLE IF EXISTS config_entries;
