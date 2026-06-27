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

// BootstrapAuth is the one-time credential for the fresh box — exactly one of Password or
// PrivateKey. It is held in memory for the active setup attempt only: never written to disk,
// never persisted, never logged. See docs/architecture/server-management.md.
type BootstrapAuth struct {
	Password             string
	PrivateKey           []byte
	PrivateKeyPassphrase string
}

// StartSetupInput starts a dashboard-managed bootstrap run for an existing server.
type StartSetupInput struct {
	ServerID string
	Host     string
	Port     int // defaults to 22 when 0
	Username string
	Auth     BootstrapAuth
}

// SetupRun is one asynchronous bootstrap attempt's lifecycle. It carries no credential
// material — only status, a plain-English failure reason, and timestamps.
type SetupRun struct {
	ID            string
	ServerID      string
	WorkspaceID   string
	Status        string // queued | running | succeeded | failed
	FailureReason string
	StartedBy     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	FinishedAt    *time.Time
}

// SetupEvent is one ordered, redacted status/log line of a run.
type SetupEvent struct {
	Seq       int64
	Step      string
	Kind      string // status | log
	Status    string // started | ok | failed | skipped
	Message   string
	CreatedAt time.Time
}

// Service is the credential lifecycle. Get, Rotate, and Revoke are the user-driven RPC
// surface. Provision, MarkUsed, RecordFailedAuth, and OpenPrivateKey are in-process only —
// called by the SSH bootstrap runner, never exposed as RPCs. OpenPrivateKey returns opened
// private-key bytes and so must never cross the API boundary.
type Service interface {
	Provision(ctx context.Context, in ProvisionInput) (Credential, error)
	Rotate(ctx context.Context, in RotateInput) (Credential, error)
	Revoke(ctx context.Context, in RevokeInput) error
	Get(ctx context.Context, serverID string) (Credential, error)
	MarkUsed(ctx context.Context, in UseInput) error
	RecordFailedAuth(ctx context.Context, in FailedAuthInput) error
	OpenPrivateKey(ctx context.Context, serverID string) ([]byte, error)

	// StartSetup begins an asynchronous bootstrap run over SSH using the one-time bootstrap
	// credential, which is used only for the attempt and never stored. GetSetupRun and
	// ListSetupEvents expose the run's redacted progress to the dashboard.
	StartSetup(ctx context.Context, in StartSetupInput) (SetupRun, error)
	GetSetupRun(ctx context.Context, setupRunID string) (SetupRun, error)
	ListSetupEvents(ctx context.Context, setupRunID string, afterSeq int64) ([]SetupEvent, error)
}
