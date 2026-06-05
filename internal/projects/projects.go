// Package projects owns projects and the workspace aggregate: workspaces and
// their membership/roles. It is a PRIVILEGED module — every mutation is authorized
// via the neutral authz.Authorizer port (satisfied by the policy module) before it
// runs, and audited in the same transaction. See docs/architecture/modules.md.
package projects

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Project is the domain model (independent of DB and transport types).
type Project struct {
	ID          string
	WorkspaceID string
	Name        string
	Slug        string
	CreatedAt   time.Time
}

// CreateInput is the data needed to create a project. The actor is the
// authenticated caller, read from the request context.
type CreateInput struct {
	WorkspaceID string
	Name        string
}

// Workspace is the top-level container a user owns or belongs to.
type Workspace struct {
	ID        string
	Name      string
	Slug      string
	CreatedAt time.Time
}

// Member is a user's membership of a workspace.
type Member struct {
	UserID    string
	Email     string
	Role      string
	CreatedAt time.Time
}

// CreateWorkspaceInput creates a workspace owned by the caller.
type CreateWorkspaceInput struct {
	Name string
}

// InviteMemberInput adds an existing user (by email) to a workspace at a role.
type InviteMemberInput struct {
	WorkspaceID string
	Email       string
	Role        string
}

// ListMembersInput lists a workspace's members.
type ListMembersInput struct {
	WorkspaceID string
}

// ChangeRoleInput changes a member's role.
type ChangeRoleInput struct {
	WorkspaceID string
	UserID      string
	Role        string
}

// RemoveMemberInput removes a member from a workspace.
type RemoveMemberInput struct {
	WorkspaceID string
	UserID      string
}

// Service is the surface other code (handlers, the CLI, internal/app, the auth
// module's bootstrapper port) depends on.
type Service interface {
	Create(ctx context.Context, in CreateInput) (Project, error)
	Get(ctx context.Context, id string) (Project, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Project, error)

	CreateWorkspace(ctx context.Context, in CreateWorkspaceInput) (Workspace, error)
	// CreateInitialWorkspace creates a workspace and owner membership inside an
	// existing transaction, deliberately skipping authorization. It exists only for
	// the auth module's registration to bootstrap a new user's first workspace, and
	// must only ever make the given user the owner of a brand-new workspace.
	CreateInitialWorkspace(ctx context.Context, tx database.Tx, userID, name, actor string) (workspaceID string, err error)
	ListMyWorkspaces(ctx context.Context, userID string) ([]Workspace, error)
	InviteMember(ctx context.Context, in InviteMemberInput) error
	ListMembers(ctx context.Context, in ListMembersInput) ([]Member, error)
	ChangeMemberRole(ctx context.Context, in ChangeRoleInput) error
	RemoveMember(ctx context.Context, in RemoveMemberInput) error
}
