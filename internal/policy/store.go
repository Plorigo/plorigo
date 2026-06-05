package policy

import "context"

// MembershipReader is the CONSUMER-DEFINED port policy needs: the caller's role in
// a workspace. *membership.Service satisfies it structurally — policy never imports
// membership; the boundary is wired in internal/app.
type MembershipReader interface {
	RoleForUser(ctx context.Context, workspaceID, userID string) (role string, ok bool, err error)
}
