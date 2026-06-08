// Package secrets owns encrypted, per-environment secret values: write-only key/value
// pairs scoped to an environment within the workspace -> project -> environment model.
// It is a PRIVILEGED, environment-scoped module: every mutation is authorized via the
// neutral authz.Authorizer port (satisfied by the policy module) before it runs, and
// audited in the same transaction. Because a secret is environment-scoped, the owning
// workspace is resolved through the parent environment's project (see store.go).
//
// Values are ENCRYPTED at rest (AES-256-GCM via the Sealer port, keyed by
// APP_MASTER_KEY) and WRITE-ONLY: a value is accepted on Set but is never returned by
// Set or List and is never logged. List yields metadata only (key + timestamps).
// Non-secret configuration lives in the sibling envvars module — this is its encrypted
// counterpart. See docs/architecture/security.md and modules.md.
package secrets

import (
	"context"
	"time"
)

// Secret is the domain model (independent of DB and transport types). It carries NO
// value: the plaintext is sealed on write and never read back. WorkspaceID is the
// owning workspace, resolved through the parent environment's project; it is used to
// authorize and audit, and is not part of the wire contract.
type Secret struct {
	ID            string
	EnvironmentID string
	WorkspaceID   string
	Key           string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SetInput is the data needed to create or update a secret. Value is the plaintext to
// seal; it is never stored or logged in the clear. The actor is the authenticated
// caller, read from the request context.
type SetInput struct {
	EnvironmentID string
	Key           string
	Value         string
}

// DeleteInput identifies the secret to delete, by environment and key.
type DeleteInput struct {
	EnvironmentID string
	Key           string
}

// Service is the surface other code (handlers, internal/app, tests) depends on. Set
// and List return metadata only — never the secret value.
type Service interface {
	Set(ctx context.Context, in SetInput) (Secret, error)
	List(ctx context.Context, environmentID string) ([]Secret, error)
	Delete(ctx context.Context, in DeleteInput) error
}
