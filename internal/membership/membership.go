// Package membership is a provider-only read module over workspace membership.
// The projects module owns the writes (see docs/architecture/control-plane.md);
// membership exposes the role and workspace-list reads that the policy module and
// the dashboard need — so policy can authorize without importing projects. Like
// audit, it has no ConnectRPC surface. See docs/architecture/modules.md.
package membership

import (
	"context"
	"log/slog"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Deps are what the membership module needs from the platform.
type Deps struct {
	DB  *database.DB
	Log *slog.Logger
}

// Service answers membership read questions.
type Service struct {
	store Store
	log   *slog.Logger
}

// New builds the membership service.
func New(d Deps) *Service {
	return &Service{store: newPostgresStore(d.DB), log: d.Log}
}

// RoleForUser returns the user's role in the workspace. ok is false (with a nil
// error) when the user is not a member. This is the single question the policy
// module asks of membership.
func (s *Service) RoleForUser(ctx context.Context, workspaceID, userID string) (string, bool, error) {
	return s.store.RoleForUser(ctx, workspaceID, userID)
}
