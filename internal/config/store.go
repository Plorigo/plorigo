package config

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port the service needs. Implemented by postgres.go, faked in
// tests. Mutations take a database.Tx so they commit with the audit row. For secrets the
// store only ever sees ciphertext — the service seals the plaintext before calling it.
// Exactly one of value/ciphertext is non-nil on an upsert, matching typ.
type Store interface {
	UpsertServiceConfig(ctx context.Context, tx database.Tx, typ Type, serviceID, key string, value *string, ciphertext []byte) (Entry, error)
	UpsertEnvironmentConfig(ctx context.Context, tx database.Tx, typ Type, environmentID, key string, value *string, ciphertext []byte) (Entry, error)
	// ListForService returns the service's service-level entries plus the environment-shared
	// entries for the service's environment (the store resolves the environment). Secret
	// entries carry no value.
	ListForService(ctx context.Context, serviceID string) ([]Entry, error)
	// DeleteServiceConfig / DeleteEnvironmentConfig remove the (scope target, key) row. ok is
	// false (nil error) when no row matched, so a delete that removed nothing is reported as
	// NotFound rather than audited as a change. deletedID is the removed row's id.
	DeleteServiceConfig(ctx context.Context, tx database.Tx, serviceID, key string) (deletedID string, ok bool, err error)
	DeleteEnvironmentConfig(ctx context.Context, tx database.Tx, environmentID, key string) (deletedID string, ok bool, err error)
	// WorkspaceIDForService / WorkspaceIDForEnvironment resolve the owning workspace (the
	// service row denormalizes it; the environment resolves it through its project), so this
	// module authorizes and audits against the workspace. ok is false (nil error) when the
	// service / environment does not exist.
	WorkspaceIDForService(ctx context.Context, serviceID string) (workspaceID string, ok bool, err error)
	WorkspaceIDForEnvironment(ctx context.Context, environmentID string) (workspaceID string, ok bool, err error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared here as a
// port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what config needs from the audit module.
// *audit.Service satisfies it structurally — config never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// Sealer is the CONSUMER-DEFINED port for encrypting a secret value at rest. *crypto.Box
// satisfies it structurally — config never imports platform/crypto, and the master key
// stays in that package. Only Seal is needed here; opening ciphertext is a deploy-time
// concern (the deployments module), never an API read.
type Sealer interface {
	Seal(plaintext []byte) ([]byte, error)
}
