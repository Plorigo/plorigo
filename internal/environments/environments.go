// Package environments owns environments: a deployment target within a project
// (preview / staging / production / custom) — the third leg of the
// workspace -> project -> environment model. It is a PRIVILEGED, workspace-scoped
// module: every mutation is authorized via the neutral authz.Authorizer port
// (satisfied by the policy module) before it runs, and audited in the same
// transaction. Because an environment is project-scoped, the owning workspace is
// resolved through the parent project (see store.go). See docs/architecture/modules.md.
package environments

import (
	"context"
	"time"
)

// Environment is the domain model (independent of DB and transport types).
// WorkspaceID is the owning workspace, resolved through the parent project; it is
// used to authorize and audit, and is not part of the wire contract.
type Environment struct {
	ID          string
	ProjectID   string
	WorkspaceID string
	Name        string
	Slug        string
	Type        string
	CreatedAt   time.Time
}

// CreateInput is the data needed to create an environment. The actor is the
// authenticated caller, read from the request context.
type CreateInput struct {
	ProjectID string
	Name      string
	Type      string // optional; defaults to "preview"
}

// Service is the surface other code (handlers, internal/app, tests) depends on.
type Service interface {
	Create(ctx context.Context, in CreateInput) (Environment, error)
	Get(ctx context.Context, id string) (Environment, error)
	ListByProject(ctx context.Context, projectID string) ([]Environment, error)
}
