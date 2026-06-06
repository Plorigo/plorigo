package membership

import "context"

// Store is the read-only repository port. Implemented by postgres.go.
type Store interface {
	RoleForUser(ctx context.Context, workspaceID, userID string) (role string, ok bool, err error)
}
