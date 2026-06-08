// Package envvars owns non-secret, per-environment configuration: key/value pairs
// scoped to an environment within the workspace -> project -> environment model. It
// is a PRIVILEGED, environment-scoped module: every mutation is authorized via the
// neutral authz.Authorizer port (satisfied by the policy module) before it runs, and
// audited in the same transaction. Because an env var is environment-scoped, the
// owning workspace is resolved through the parent environment's project (see store.go).
//
// Values are NON-SECRET: they are stored in plaintext and returned on read. Encrypted
// secrets (write-only, versioned, redacted) are a separate module and table — keep
// secret values out of here. As a habit shared with that future module, service.go
// never logs a value. See docs/architecture/modules.md and docs/architecture/security.md.
package envvars

import (
	"context"
	"time"
)

// EnvVar is the domain model (independent of DB and transport types). WorkspaceID is
// the owning workspace, resolved through the parent environment's project; it is used
// to authorize and audit, and is not part of the wire contract.
type EnvVar struct {
	ID            string
	EnvironmentID string
	WorkspaceID   string
	Key           string
	Value         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SetInput is the data needed to create or update an env var. The actor is the
// authenticated caller, read from the request context.
type SetInput struct {
	EnvironmentID string
	Key           string
	Value         string
}

// DeleteInput identifies the env var to delete, by environment and key.
type DeleteInput struct {
	EnvironmentID string
	Key           string
}

// Service is the surface other code (handlers, internal/app, tests) depends on.
type Service interface {
	Set(ctx context.Context, in SetInput) (EnvVar, error)
	List(ctx context.Context, environmentID string) ([]EnvVar, error)
	Delete(ctx context.Context, in DeleteInput) error
}
