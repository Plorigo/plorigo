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
	// A user-scoped event (e.g. login) has no workspace; store NULL rather than a
	// fake id. Workspace-scoped actions always pass their real workspace id.
	var workspaceID *string
	if e.WorkspaceID != "" {
		workspaceID = &e.WorkspaceID
	}
	_, err := db.New(tx).CreateAuditEvent(ctx, db.CreateAuditEventParams{
		WorkspaceID: workspaceID,
		Actor:       e.Actor,
		Action:      e.Action,
		TargetType:  e.TargetType,
		TargetID:    e.TargetID,
	})
	return err
}
