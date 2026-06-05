package audit

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store. The only file in the module allowed to import
// internal/platform/database/db (enforced by depguard). Insert always runs within
// the caller's transaction.
type postgresStore struct{}

func newPostgresStore() *postgresStore { return &postgresStore{} }

func (s *postgresStore) Insert(ctx context.Context, tx database.Tx, e Event) error {
	_, err := db.New(tx).CreateAuditEvent(ctx, db.CreateAuditEventParams{
		WorkspaceID: e.WorkspaceID,
		Actor:       e.Actor,
		Action:      e.Action,
		TargetType:  e.TargetType,
		TargetID:    e.TargetID,
	})
	return err
}
