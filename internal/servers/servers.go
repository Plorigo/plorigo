// Package servers owns connected servers: a machine (VPS / bare metal) a workspace
// deploys its apps onto — a top-level entity under a workspace, like a project. It is a
// PRIVILEGED, workspace-scoped module: every mutation is authorized via the neutral
// authz.Authorizer port (satisfied by the policy module) before it runs, and audited in
// the same transaction. See docs/architecture/modules.md.
package servers

import (
	"context"
	"time"
)

// Server is the domain model (independent of DB and transport types).
type Server struct {
	ID          string
	WorkspaceID string
	Name        string
	Slug        string
	CreatedAt   time.Time
}

// CreateInput is the data needed to create a server. The actor is the authenticated
// caller, read from the request context.
type CreateInput struct {
	WorkspaceID string
	Name        string
}

// Service is the surface other code (handlers, internal/app, tests) depends on.
type Service interface {
	Create(ctx context.Context, in CreateInput) (Server, error)
	Get(ctx context.Context, id string) (Server, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Server, error)
	// Delete removes a server; its agent registration and deployment history cascade
	// with it (see migrations 00008/00009).
	Delete(ctx context.Context, id string) error
}
