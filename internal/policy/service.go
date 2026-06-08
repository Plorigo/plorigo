package policy

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

var _ authz.Authorizer = (*Service)(nil)

// permissions is the role -> allowed-actions matrix. Each role lists its actions
// explicitly (no implicit inheritance) so the grant is auditable at a glance.
// Finer rules that depend on the *target* (e.g. an admin may not remove an owner)
// live in the owning service, not here.
var permissions = map[string]map[authz.Action]bool{
	authz.RoleOwner: {
		authz.ActionMemberInvite:      true,
		authz.ActionMemberRemove:      true,
		authz.ActionMemberRoleChange:  true,
		authz.ActionMemberList:        true,
		authz.ActionProjectCreate:     true,
		authz.ActionProjectRead:       true,
		authz.ActionProjectDelete:     true,
		authz.ActionEnvironmentCreate: true,
		authz.ActionEnvironmentRead:   true,
	},
	authz.RoleAdmin: {
		authz.ActionMemberInvite:      true,
		authz.ActionMemberRemove:      true,
		authz.ActionMemberList:        true,
		authz.ActionProjectCreate:     true,
		authz.ActionProjectRead:       true,
		authz.ActionProjectDelete:     true,
		authz.ActionEnvironmentCreate: true,
		authz.ActionEnvironmentRead:   true,
	},
	authz.RoleMember: {
		authz.ActionMemberList:        true,
		authz.ActionProjectCreate:     true,
		authz.ActionProjectRead:       true,
		authz.ActionEnvironmentCreate: true,
		authz.ActionEnvironmentRead:   true,
	},
	authz.RoleViewer: {
		authz.ActionMemberList:      true,
		authz.ActionProjectRead:     true,
		authz.ActionEnvironmentRead: true,
	},
}

// Authorize returns nil if p may perform action on res, or a PermissionDenied
// problem otherwise. Authorization is workspace-scoped: the principal's role is
// looked up for res.WorkspaceID. A missing membership denies.
func (s *Service) Authorize(ctx context.Context, p principal.Principal, action authz.Action, res authz.Resource) error {
	if !p.IsAuthenticated() {
		return problem.PermissionDenied("authentication required")
	}
	if res.WorkspaceID == "" {
		return problem.PermissionDenied("a workspace is required to authorize %q", string(action))
	}
	role, ok, err := s.members.RoleForUser(ctx, res.WorkspaceID, p.UserID)
	if err != nil {
		return problem.Internalf(err, "authorize %q", string(action))
	}
	if !ok {
		return problem.PermissionDenied("you are not a member of this workspace")
	}
	if permissions[role][action] {
		return nil
	}
	return problem.PermissionDenied("role %q cannot perform %q", role, string(action))
}
