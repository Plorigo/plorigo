package audit

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port. Implemented by postgres.go. Insert always runs
// within the caller's transaction.
type Store interface {
	Insert(ctx context.Context, tx database.Tx, e Event) error
}
