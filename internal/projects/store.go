package projects

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port the service needs. Implemented by postgres.go,
// faked in tests. Mutations take a database.Tx so they commit with the audit row.
type Store interface {
	InsertProject(ctx context.Context, tx database.Tx, p Project) (Project, error)
	GetProject(ctx context.Context, id string) (Project, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Project, error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared
// here as a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what projects needs from the audit
// module. *audit.Service satisfies it structurally — projects never imports audit.
// This is what lets depguard forbid all cross-module imports (see .golangci.yml).
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
