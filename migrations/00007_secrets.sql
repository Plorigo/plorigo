-- +goose Up
-- An encrypted, per-environment secret. Unlike env_vars, the value is stored as
-- ciphertext (AES-256-GCM, sealed by the control plane with APP_MASTER_KEY) and is
-- WRITE-ONLY: it is never read back through the API. Secrets are mutable, so they
-- carry updated_at. Authorization is workspace-scoped; the owning workspace is
-- resolved through environment -> project. See docs/architecture/security.md and
-- data-and-api.md.
CREATE TABLE secrets (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id uuid NOT NULL REFERENCES environments (id) ON DELETE CASCADE,
    -- Defense-in-depth: the key grammar and length bounds are validated in Go, but
    -- the CHECK guarantees no malformed key can ever be written (cf. 00006_envvars.sql).
    key            text NOT NULL
        CHECK (key ~ '^[A-Z_][A-Z0-9_]*$' AND char_length(key) <= 128),
    -- Ciphertext only — never plaintext. The plaintext length is bounded in Go; the
    -- CHECK bounds the sealed bytes (nonce + ciphertext + GCM tag) as a backstop.
    ciphertext     bytea NOT NULL
        CHECK (octet_length(ciphertext) <= 65536),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (environment_id, key)
);

CREATE INDEX idx_secrets_environment_id ON secrets (environment_id);

-- +goose Down
DROP TABLE IF EXISTS secrets;
