// Package policy is the authorization decision point: given a principal, an
// action, and a resource, it answers yes or no. It implements authz.Authorizer, so
// privileged modules depend on the neutral authz port and never import policy. It
// reads roles through a consumer-defined MembershipReader (satisfied by the
// membership module). Decision-only: it owns no tables and has no ConnectRPC
// surface. See docs/architecture/security.md and modules.md.
package policy

import "log/slog"

// Deps are what the policy module needs.
type Deps struct {
	Membership MembershipReader
	Log        *slog.Logger
}

// Service is the authorization service. It implements authz.Authorizer.
type Service struct {
	members MembershipReader
	log     *slog.Logger
}

// New builds the policy service.
func New(d Deps) *Service {
	return &Service{members: d.Membership, log: d.Log}
}
