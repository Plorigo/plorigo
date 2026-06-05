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

	InsertWorkspace(ctx context.Context, tx database.Tx, name, slug string) (Workspace, error)
	AddMember(ctx context.Context, tx database.Tx, workspaceID, userID, role string) error
	MemberRole(ctx context.Context, workspaceID, userID string) (role string, ok bool, err error)
	ListWorkspacesForUser(ctx context.Context, userID string) ([]Workspace, error)
	ListMembers(ctx context.Context, workspaceID string) ([]Member, error)
	UpdateMemberRole(ctx context.Context, tx database.Tx, workspaceID, userID, role string) error
	RemoveMember(ctx context.Context, tx database.Tx, workspaceID, userID string) error
	UserIDByEmail(ctx context.Context, email string) (userID string, ok bool, err error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared
// here as a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what projects needs from the audit
// module. *audit.Service satisfies it structurally — projects never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
