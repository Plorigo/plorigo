-- +goose Up
-- The persistent SSH management credential for a dashboard-managed server. This is the
-- non-root `plorigo` user's keypair the control plane uses for setup/repair over the
-- inbound SSH channel — deliberately distinct from the agent's job-signing key and never
-- the deploy path. See docs/architecture/server-management.md.
--
-- Security model (mirrors `secrets`):
--   * The private key is stored ONLY as ciphertext (AES-256-GCM, sealed by the control
--     plane with APP_MASTER_KEY) and is WRITE-ONLY — never returned through any RPC.
--   * The public key (authorized_keys line) and fingerprint are non-secret metadata.
--   * The raw bootstrap credential the user supplies is NEVER a column here: it lives in
--     memory for the active setup attempt only and is discarded on success and failure.
--   * Authorization/audit are workspace-scoped, resolved through servers.workspace_id.
-- One credential per server; rotation replaces the row's key material in place.
CREATE TABLE ssh_management_keys (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id          uuid NOT NULL REFERENCES servers (id) ON DELETE CASCADE,
    -- SHA256 fingerprint of the management public key (e.g. "SHA256:…"), surfaced in the
    -- dashboard so a user can verify which key is installed.
    fingerprint        text NOT NULL,
    -- The public OpenSSH authorized_keys line ("ssh-ed25519 AAAA…"). Non-secret.
    public_key         text NOT NULL,
    -- nonce||ciphertext of the OpenSSH PEM private key. Write-only; an ed25519 key seals
    -- to a few hundred bytes, so the CHECK is a generous backstop, not a tuning knob.
    sealed_private_key bytea NOT NULL
        CHECK (octet_length(sealed_private_key) <= 4096),
    -- Where the credential is in its rotation lifecycle. 'rotating'/'superseded' are
    -- reserved for the SSH bootstrap runner (which installs/removes keys on the server);
    -- this storage layer only ever sets 'active'.
    rotation_state     text NOT NULL DEFAULT 'active'
        CHECK (rotation_state IN ('active', 'rotating', 'superseded')),
    last_used_at       timestamptz,
    rotated_at         timestamptz,
    -- Revocation state: NULL means active, non-NULL means access has been cut off.
    revoked_at         timestamptz,
    -- The user who provisioned the credential. SET NULL on user delete so the credential
    -- (and its server) survive; the authoritative actor trail is the audit log.
    created_by         uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (server_id)
);

CREATE INDEX idx_ssh_management_keys_server_id ON ssh_management_keys (server_id);

-- +goose Down
DROP TABLE IF EXISTS ssh_management_keys;
