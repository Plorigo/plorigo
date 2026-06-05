// Package audit owns the append-only audit trail of sensitive actions. It exposes
// a concrete *Service whose Record method satisfies the consumer-defined recorder
// ports of other modules (e.g. projects.Recorder) — modules never import each other.
//
// audit is a provider-only module: it has no ConnectRPC handler/Route, so it is the
// minimal shape of a module (domain + port + adapter), not the full template.
package audit

import (
	"context"
	"log/slog"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Deps are what the audit module needs from the platform.
type Deps struct {
	Log *slog.Logger
}

// Service records audit events.
type Service struct {
	store Store
	log   *slog.Logger
}

// New builds the audit service.
func New(d Deps) *Service {
	return &Service{store: newPostgresStore(), log: d.Log}
}

// Record appends an event within the caller's transaction, so an action and its
// audit row commit together. The signature is what consumers' recorder ports expect.
func (s *Service) Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error {
	return s.store.Insert(ctx, tx, Event{
		WorkspaceID: workspaceID,
		Actor:       actor,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
	})
}

// Event is a single entry in the audit trail.
type Event struct {
	WorkspaceID string
	Actor       string
	Action      string
	TargetType  string
	TargetID    string
}
