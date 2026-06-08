package secrets

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port the service needs. Implemented by postgres.go, faked
// in tests. Mutations take a database.Tx so they commit with the audit row. The store
// only ever sees ciphertext — the service seals the plaintext before calling it.
type Store interface {
	UpsertSecret(ctx context.Context, tx database.Tx, environmentID, key string, ciphertext []byte) (Secret, error)
	ListByEnvironment(ctx context.Context, environmentID string) ([]Secret, error)
	// DeleteSecret removes the (environment, key) row. ok is false (with a nil error)
	// when no row matched, so a delete that removed nothing is reported as NotFound
	// rather than audited as a change. deletedID is the removed row's id.
	DeleteSecret(ctx context.Context, tx database.Tx, environmentID, key string) (deletedID string, ok bool, err error)
	// WorkspaceIDForEnvironment resolves an environment's owning workspace (through its
	// parent project), so this environment-scoped module can authorize and audit
	// against the workspace. ok is false (with a nil error) when the environment does
	// not exist.
	WorkspaceIDForEnvironment(ctx context.Context, environmentID string) (workspaceID string, ok bool, err error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared here
// as a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what secrets needs from the audit module.
// *audit.Service satisfies it structurally — secrets never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// Sealer is the CONSUMER-DEFINED port for encrypting a secret value at rest.
// *crypto.Box satisfies it structurally — secrets never imports platform/crypto, and
// the master key stays in that package. Only Seal is needed here; opening ciphertext
// is a future deploy-time concern, never an API read.
type Sealer interface {
	Seal(plaintext []byte) ([]byte, error)
}
