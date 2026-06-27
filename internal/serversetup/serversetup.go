// Package serversetup owns the persistent SSH management credential created during
// dashboard-managed server setup: the non-root `plorigo` user's keypair the control plane
// uses to bootstrap and repair a server over the inbound SSH channel. It stores the
// private key sealed at rest (AES-256-GCM, APP_MASTER_KEY) and write-only — never returned
// through any RPC — and governs its lifecycle (provision, rotate, revoke) with workspace-
// scoped authorization and an audit record for every change and use. The actual SSH
// connection that installs/removes keys and runs setup is a separate bootstrap runner; this
// module is the credential store and lifecycle it builds on. See
// docs/architecture/server-management.md and security.md.
package serversetup

import (
	"context"
	"time"
)

// Credential is the NON-SECRET metadata for a server's SSH management credential. It
// deliberately carries no private key material, so a Credential can never leak the key
// through a log line or an RPC response — the sealed key is handled only inside the store
// and opened only in-process via OpenPrivateKey.
type Credential struct {
	ID            string
	ServerID      string
	WorkspaceID   string // resolved from the owning server; not a column on the row
	Fingerprint   string
	PublicKey     string // OpenSSH authorized_keys line (public)
	RotationState string // active | rotating | superseded
	LastUsedAt    *time.Time
	RotatedAt     *time.Time
	RevokedAt     *time.Time
	CreatedBy     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Revoked reports whether the management channel has been cut off.
func (c Credential) Revoked() bool { return c.RevokedAt != nil }

// ProvisionInput identifies the server to provision a management credential for.
type ProvisionInput struct{ ServerID string }

// RotateInput identifies the server whose management credential to rotate.
type RotateInput struct{ ServerID string }

// RevokeInput identifies the server whose management credential to revoke.
type RevokeInput struct{ ServerID string }

// UseInput identifies the server whose management credential was just used.
type UseInput struct{ ServerID string }

// FailedAuthInput records an SSH authentication failure against a server. Reason is a
// short, non-sensitive hint (e.g. "host key mismatch") — it must never carry a credential,
// password, or token.
type FailedAuthInput struct {
	ServerID string
	Reason   string
}

// Service is the credential lifecycle. Get is the only user-driven RPC surface (read-only
// metadata). Provision, Rotate, Revoke, MarkUsed, RecordFailedAuth, and OpenPrivateKey are
// in-process only — called by the SSH bootstrap runner, never exposed as RPCs. Rotate and
// Revoke are runner-only because each must also install/remove the key on the server in the
// same operation; exposed standalone they would desync the stored credential from the
// server's authorized_keys (see serversetup.proto). OpenPrivateKey returns opened
// private-key bytes and so must never cross the API boundary.
type Service interface {
	Provision(ctx context.Context, in ProvisionInput) (Credential, error)
	Rotate(ctx context.Context, in RotateInput) (Credential, error)
	Revoke(ctx context.Context, in RevokeInput) error
	Get(ctx context.Context, serverID string) (Credential, error)
	MarkUsed(ctx context.Context, in UseInput) error
	RecordFailedAuth(ctx context.Context, in FailedAuthInput) error
	OpenPrivateKey(ctx context.Context, serverID string) ([]byte, error)
}
