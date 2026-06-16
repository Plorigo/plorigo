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
	ActionServerDelete      Action = "server.delete"
	// Agent actions cover minting a registration token (connecting a server) and
	// reading agent liveness. The agent-facing Register/Heartbeat RPCs are not
	// user-scoped and authenticate by their own token/credential, so they have no
	// Action here.
	ActionAgentCreate  Action = "agent.create"
	ActionAgentRead    Action = "agent.read"
	ActionEnvVarSet    Action = "env_var.set"
	ActionEnvVarRead   Action = "env_var.read"
	ActionEnvVarDelete Action = "env_var.delete"
	// Secret actions never expose a value — there is no "read" action because
	// secrets are write-only. ActionSecretList covers metadata (keys + timestamps).
	ActionSecretSet    Action = "secret.set"
	ActionSecretList   Action = "secret.list"
	ActionSecretDelete Action = "secret.delete"
	// Deployment actions cover triggering a deploy and reading deployment status,
	// timeline, and logs. The agent-facing Poll/Report RPCs are not user-scoped and
	// authenticate by the agent credential, so they have no Action here.
	ActionDeploymentCreate Action = "deployment.create"
	ActionDeploymentRead   Action = "deployment.read"
	// Source actions cover connecting a workspace's Git provider (GitHub OAuth) and a
	// project's repository, reading that metadata, and disconnecting. Connect also gates
	// listing repositories/branches, since those calls use the connection's token.
	ActionSourceConnect    Action = "source.connect"
	ActionSourceRead       Action = "source.read"
	ActionSourceDisconnect Action = "source.disconnect"
	// Service actions cover creating a service (its source plus an optional first deploy),
	// reading it, updating its source/visibility, and deleting it. Like deployment actions,
	// the agent-facing Poll/Report RPCs are not user-scoped and have no Action here.
	ActionServiceCreate Action = "service.create"
	ActionServiceRead   Action = "service.read"
	ActionServiceUpdate Action = "service.update"
	ActionServiceDelete Action = "service.delete"
	// Domain actions cover attaching custom hostnames to services and checking DNS. Route
	// sync is agent-facing and authenticated by agent credential, so it has no Action here.
	ActionDomainCreate Action = "domain.create"
	ActionDomainRead   Action = "domain.read"
	ActionDomainVerify Action = "domain.verify"
	ActionDomainDelete Action = "domain.delete"
	// Server-setup actions govern the persistent SSH management credential created during
	// dashboard-managed server setup. Opening an inbound SSH channel is an admin-tier
	// capability, so running the channel (ActionServerSetupRun: provision + record use /
	// failed auth) and the destructive lifecycle ops (rotate, revoke) are owner/admin only;
	// the self-serve one-line install stores no credential and needs none of these. Reading
	// exposes only non-secret metadata (fingerprint, timestamps, rotation/revocation state) —
	// the private key is never an action target, since it is write-only and never returned.
	// See docs/architecture/server-management.md.
	ActionServerSetupRun       Action = "server_setup.run"
	ActionServerSetupKeyRotate Action = "server_setup.key.rotate"
	ActionServerSetupKeyRevoke Action = "server_setup.key.revoke"
	ActionServerSetupKeyRead   Action = "server_setup.key.read"
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
