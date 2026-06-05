// Package projects is the reference module: it shows the file layout, the
// consumer-defined-port pattern, and the transactional write-with-audit pattern
// that every other control-plane module copies. See docs/architecture/modules.md.
package projects

import (
	"context"
	"time"
)

// Project is the domain model (independent of DB and transport types).
type Project struct {
	ID          string
	WorkspaceID string
	Name        string
	Slug        string
	CreatedAt   time.Time
}

// CreateInput is the data needed to create a project.
type CreateInput struct {
	WorkspaceID string
	Name        string
	// Actor identifies who performed the action, for the audit trail. In the
	// scaffold this is "system"; with auth it becomes the authenticated caller.
	Actor string
}

// Service is the only surface other code (the handler, internal/app) depends on.
type Service interface {
	Create(ctx context.Context, in CreateInput) (Project, error)
	Get(ctx context.Context, id string) (Project, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Project, error)
}
