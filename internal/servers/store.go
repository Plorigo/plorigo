package servers

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port the service needs. Implemented by postgres.go, faked in
// tests. Mutations take a database.Tx so they commit with the audit row.
type Store interface {
	InsertServer(ctx context.Context, tx database.Tx, s Server) (Server, error)
	GetServer(ctx context.Context, id string) (Server, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Server, error)
	// DeleteServer removes the row. ok is false (with a nil error) when no row matched,
	// so a delete that removed nothing is reported as NotFound rather than audited.
	DeleteServer(ctx context.Context, tx database.Tx, id string) (ok bool, err error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared here as
// a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what servers needs from the audit module.
// *audit.Service satisfies it structurally — servers never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
