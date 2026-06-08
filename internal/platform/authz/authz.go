// Package authz is the neutral authorization vocabulary: the actions and
// resources a caller may be authorized for, plus the Authorizer port. The policy
// module implements Authorizer; privileged modules depend on this package — so no
// module imports policy directly, and the projects<->policy relationship needs no
// cross-module import. See docs/architecture/security.md and modules.md.
package authz

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/principal"
)

// Action is a verb a caller may attempt. Actions are workspace-scoped. The string
// values may appear in audit records, so treat them as part of the contract.
type Action string

// The actions the policy module knows how to authorize.
const (
	ActionWorkspaceCreate   Action = "workspace.create"
	ActionMemberInvite      Action = "workspace.member.invite"
	ActionMemberRemove      Action = "workspace.member.remove"
	ActionMemberRoleChange  Action = "workspace.member.role.change"
	ActionMemberList        Action = "workspace.member.list"
	ActionProjectCreate     Action = "project.create"
	ActionProjectRead       Action = "project.read"
	ActionProjectDelete     Action = "project.delete"
	ActionEnvironmentCreate Action = "environment.create"
	ActionEnvironmentRead   Action = "environment.read"
	ActionServerCreate      Action = "server.create"
	ActionServerRead        Action = "server.read"
	ActionEnvVarSet         Action = "env_var.set"
	ActionEnvVarRead        Action = "env_var.read"
	ActionEnvVarDelete      Action = "env_var.delete"
)

// Workspace roles, most privileged first. Stored in workspace_members.role and
// shared between the policy module (which maps roles to permissions) and the
// projects module (which assigns and validates them) — neither imports the other.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// ValidRole reports whether r is a known role.
func ValidRole(r string) bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return true
	}
	return false
}

// Resource identifies what an Action targets. Authorization is scoped to
// WorkspaceID; Type and ID are informational (auditing, future finer checks).
type Resource struct {
	Type        string
	WorkspaceID string
	ID          string
}

// Authorizer decides whether a principal may perform an action on a resource.
// Implemented by the policy module. Returns nil if allowed, or a
// problem.PermissionDenied error if not.
type Authorizer interface {
	Authorize(ctx context.Context, p principal.Principal, action Action, res Resource) error
}
