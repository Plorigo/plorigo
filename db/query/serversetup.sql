-- name: UpsertSSHManagementKey :one
-- Provision (or re-provision) a server's SSH management credential, replacing any prior
-- key material in place and clearing rotation/revocation state. RETURNING yields metadata
-- only — never the sealed private key, which is write-only.
INSERT INTO ssh_management_keys (server_id, fingerprint, public_key, sealed_private_key, created_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (server_id) DO UPDATE SET
    fingerprint = EXCLUDED.fingerprint,
    public_key = EXCLUDED.public_key,
    sealed_private_key = EXCLUDED.sealed_private_key,
    created_by = EXCLUDED.created_by,
    rotation_state = 'active',
    rotated_at = NULL,
    revoked_at = NULL,
    updated_at = now()
RETURNING id, server_id, fingerprint, public_key, rotation_state, last_used_at, rotated_at, revoked_at, created_by, created_at, updated_at;

-- name: RotateSSHManagementKey :one
-- Replace the key material of a server's ACTIVE credential, stamping rotated_at. No row
-- matches a missing or revoked credential, so the caller reports NotFound. RETURNING is
-- metadata only.
UPDATE ssh_management_keys SET
    fingerprint = $2,
    public_key = $3,
    sealed_private_key = $4,
    rotation_state = 'active',
    rotated_at = now(),
    updated_at = now()
WHERE server_id = $1 AND revoked_at IS NULL
RETURNING id, server_id, fingerprint, public_key, rotation_state, last_used_at, rotated_at, revoked_at, created_by, created_at, updated_at;

-- name: RevokeSSHManagementKey :one
-- Cut off the management channel: mark the active credential revoked. Returns the row id,
-- so a no-op (already revoked / absent) is distinguished from a real revocation and never
-- audited as a change.
UPDATE ssh_management_keys SET
    revoked_at = now(),
    updated_at = now()
WHERE server_id = $1 AND revoked_at IS NULL
RETURNING id;

-- name: MarkSSHManagementKeyUsed :one
-- Stamp last_used_at after a successful SSH connection. Returns id so a no-op (absent or
-- revoked) is distinguished from a real update.
UPDATE ssh_management_keys SET
    last_used_at = now(),
    updated_at = now()
WHERE server_id = $1 AND revoked_at IS NULL
RETURNING id;

-- name: GetSSHManagementKey :one
-- Metadata only — never the sealed private key, which is write-only.
SELECT id, server_id, fingerprint, public_key, rotation_state, last_used_at, rotated_at, revoked_at, created_by, created_at, updated_at
FROM ssh_management_keys
WHERE server_id = $1;

-- name: GetSealedSSHManagementKey :one
-- The sealed private-key ciphertext for an ACTIVE credential. Used ONLY in-process by the
-- SSH runner to open the key for a connection — never returned through any RPC.
SELECT sealed_private_key
FROM ssh_management_keys
WHERE server_id = $1 AND revoked_at IS NULL;
