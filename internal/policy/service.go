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
		authz.ActionMemberInvite:         true,
		authz.ActionMemberRemove:         true,
		authz.ActionMemberRoleChange:     true,
		authz.ActionMemberList:           true,
		authz.ActionProjectCreate:        true,
		authz.ActionProjectRead:          true,
		authz.ActionProjectDelete:        true,
		authz.ActionEnvironmentCreate:    true,
		authz.ActionEnvironmentRead:      true,
		authz.ActionServerCreate:         true,
		authz.ActionServerRead:           true,
		authz.ActionServerDelete:         true,
		authz.ActionAgentCreate:          true,
		authz.ActionAgentRead:            true,
		authz.ActionConfigSet:            true,
		authz.ActionConfigRead:           true,
		authz.ActionConfigDelete:         true,
		authz.ActionDeploymentCreate:     true,
		authz.ActionDeploymentRead:       true,
		authz.ActionSourceConnect:        true,
		authz.ActionSourceRead:           true,
		authz.ActionSourceDisconnect:     true,
		authz.ActionServiceCreate:        true,
		authz.ActionServiceRead:          true,
		authz.ActionServiceUpdate:        true,
		authz.ActionServiceDelete:        true,
		authz.ActionDomainCreate:         true,
		authz.ActionDomainRead:           true,
		authz.ActionDomainVerify:         true,
		authz.ActionDomainDelete:         true,
		authz.ActionBackupCreate:         true,
		authz.ActionBackupRead:           true,
		authz.ActionServerSetupRun:       true,
		authz.ActionServerSetupKeyRotate: true,
		authz.ActionServerSetupKeyRevoke: true,
		authz.ActionServerSetupKeyRead:   true,
	},
	authz.RoleAdmin: {
		authz.ActionMemberInvite:         true,
		authz.ActionMemberRemove:         true,
		authz.ActionMemberList:           true,
		authz.ActionProjectCreate:        true,
		authz.ActionProjectRead:          true,
		authz.ActionProjectDelete:        true,
		authz.ActionEnvironmentCreate:    true,
		authz.ActionEnvironmentRead:      true,
		authz.ActionServerCreate:         true,
		authz.ActionServerRead:           true,
		authz.ActionServerDelete:         true,
		authz.ActionAgentCreate:          true,
		authz.ActionAgentRead:            true,
		authz.ActionConfigSet:            true,
		authz.ActionConfigRead:           true,
		authz.ActionConfigDelete:         true,
		authz.ActionDeploymentCreate:     true,
		authz.ActionDeploymentRead:       true,
		authz.ActionSourceConnect:        true,
		authz.ActionSourceRead:           true,
		authz.ActionSourceDisconnect:     true,
		authz.ActionServiceCreate:        true,
		authz.ActionServiceRead:          true,
		authz.ActionServiceUpdate:        true,
		authz.ActionServiceDelete:        true,
		authz.ActionDomainCreate:         true,
		authz.ActionDomainRead:           true,
		authz.ActionDomainVerify:         true,
		authz.ActionDomainDelete:         true,
		authz.ActionBackupCreate:         true,
		authz.ActionBackupRead:           true,
		authz.ActionServerSetupRun:       true,
		authz.ActionServerSetupKeyRotate: true,
		authz.ActionServerSetupKeyRevoke: true,
		authz.ActionServerSetupKeyRead:   true,
	},
	authz.RoleMember: {
		authz.ActionMemberList:        true,
		authz.ActionProjectCreate:     true,
		authz.ActionProjectRead:       true,
		authz.ActionEnvironmentCreate: true,
		authz.ActionEnvironmentRead:   true,
		authz.ActionServerCreate:      true,
		authz.ActionServerRead:        true,
		authz.ActionAgentCreate:       true,
		authz.ActionAgentRead:         true,
		authz.ActionConfigSet:         true,
		authz.ActionConfigRead:        true,
		authz.ActionConfigDelete:      true,
		authz.ActionDeploymentCreate:  true,
		authz.ActionDeploymentRead:    true,
		authz.ActionSourceConnect:     true,
		authz.ActionSourceRead:        true,
		authz.ActionSourceDisconnect:  true,
		authz.ActionServiceCreate:     true,
		authz.ActionServiceRead:       true,
		authz.ActionServiceUpdate:     true,
		authz.ActionServiceDelete:     true,
		authz.ActionDomainCreate:      true,
		authz.ActionDomainRead:        true,
		authz.ActionDomainVerify:      true,
		authz.ActionDomainDelete:      true,
		authz.ActionBackupCreate:      true,
		authz.ActionBackupRead:        true,
		// A member can connect a server via the self-serve install, but managed SSH setup
		// (provisioning/rotating/revoking the inbound credential) is admin-tier; a member may
		// still see the non-secret credential metadata.
		authz.ActionServerSetupKeyRead: true,
	},
	authz.RoleViewer: {
		authz.ActionMemberList:      true,
		authz.ActionProjectRead:     true,
		authz.ActionEnvironmentRead: true,
		authz.ActionServerRead:      true,
		authz.ActionAgentRead:       true,
		// A viewer may read config: variable values and secret metadata (a secret value is
		// never returned by any RPC), but not set or delete.
		authz.ActionConfigRead:     true,
		authz.ActionDeploymentRead: true,
		// A viewer may see the connected repository metadata but not connect, list
		// repositories (which uses the token), or disconnect.
		authz.ActionSourceRead: true,
		// A viewer may see services but not create, update, or delete them.
		authz.ActionServiceRead: true,
		// A viewer may see custom domain status and DNS instructions but not mutate or verify.
		authz.ActionDomainRead: true,
		// A viewer may see backup status but not create backups.
		authz.ActionBackupRead: true,
		// A viewer may see the SSH management credential's non-secret metadata (fingerprint,
		// timestamps, state) but never run setup, rotate, or revoke.
		authz.ActionServerSetupKeyRead: true,
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
