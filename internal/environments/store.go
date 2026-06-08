package environments

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port the service needs. Implemented by postgres.go,
// faked in tests. Mutations take a database.Tx so they commit with the audit row.
type Store interface {
	InsertEnvironment(ctx context.Context, tx database.Tx, e Environment) (Environment, error)
	GetEnvironment(ctx context.Context, id string) (Environment, error)
	ListByProject(ctx context.Context, projectID string) ([]Environment, error)
	// WorkspaceIDForProject resolves a project's owning workspace, so this
	// project-scoped module can authorize and audit against the workspace. ok is
	// false (with a nil error) when the project does not exist.
	WorkspaceIDForProject(ctx context.Context, projectID string) (workspaceID string, ok bool, err error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared
// here as a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what environments needs from the audit
// module. *audit.Service satisfies it structurally — environments never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
