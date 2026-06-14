package envvars

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port the service needs. Implemented by postgres.go, faked
// in tests. Mutations take a database.Tx so they commit with the audit row.
type Store interface {
	UpsertEnvVar(ctx context.Context, tx database.Tx, e EnvVar) (EnvVar, error)
	ListByService(ctx context.Context, serviceID string) ([]EnvVar, error)
	// DeleteEnvVar removes the (service, key) row. ok is false (with a nil error) when no
	// row matched, so a delete that removed nothing is reported as NotFound rather than
	// audited as a change. deletedID is the removed row's id.
	DeleteEnvVar(ctx context.Context, tx database.Tx, serviceID, key string) (deletedID string, ok bool, err error)
	// WorkspaceIDForService resolves a service's owning workspace (denormalized onto the
	// service row), so this service-scoped module can authorize and audit against the
	// workspace. ok is false (with a nil error) when the service does not exist.
	WorkspaceIDForService(ctx context.Context, serviceID string) (workspaceID string, ok bool, err error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared here
// as a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what envvars needs from the audit module.
// *audit.Service satisfies it structurally — envvars never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
