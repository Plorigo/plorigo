// Package envvars owns non-secret, per-SERVICE configuration: key/value pairs scoped to a
// service within the workspace -> project -> environment -> service model. It is a
// PRIVILEGED, service-scoped module: every mutation is authorized via the neutral
// authz.Authorizer port (satisfied by the policy module) before it runs, and audited in
// the same transaction. Because an env var is service-scoped, the owning workspace is
// resolved through the service row (which denormalizes it; see store.go).
//
// Values are NON-SECRET: they are stored in plaintext and returned on read. Encrypted
// secrets (write-only, versioned, redacted) are a separate module and table — keep
// secret values out of here. As a habit shared with that future module, service.go
// never logs a value. See docs/architecture/modules.md and docs/architecture/security.md.
package envvars

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// EnvVar is the domain model (independent of DB and transport types). WorkspaceID is
// the owning workspace, resolved through the parent service; it is used to authorize
// and audit, and is not part of the wire contract.
type EnvVar struct {
	ID          string
	ServiceID   string
	WorkspaceID string
	Key         string
	Value       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SetInput is the data needed to create or update an env var. The actor is the
// authenticated caller, read from the request context.
type SetInput struct {
	ServiceID string
	Key       string
	Value     string
}

// DeleteInput identifies the env var to delete, by service and key.
type DeleteInput struct {
	ServiceID string
	Key       string
}

// Service is the surface other code (handlers, internal/app, tests) depends on.
type Service interface {
	Set(ctx context.Context, in SetInput) (EnvVar, error)
	List(ctx context.Context, serviceID string) ([]EnvVar, error)
	Delete(ctx context.Context, in DeleteInput) error
	// SetWithinTx writes service env vars inside the CALLER's transaction, for another module
	// provisioning a managed service's generated config (e.g. a database's credentials) so the
	// config and the service row commit together. It performs NO authorization — the caller has
	// already authorized the service create. Keys are validated and values bounded.
	SetWithinTx(ctx context.Context, tx database.Tx, serviceID string, vars map[string]string) error
}
