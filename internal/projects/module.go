package projects

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the projects module needs. Audit and Policy are CONSUMER-DEFINED
// ports (authz.Authorizer is satisfied by *policy.Service, Recorder by
// *audit.Service), wired in internal/app — projects imports neither module.
type Deps struct {
	DB     *database.DB
	Audit  Recorder
	Policy authz.Authorizer
	Log    *slog.Logger
}

// Module is the projects module: the only wiring surface other code touches.
type Module struct {
	service Service
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service: newService(d.DB, store, d.Policy, d.Audit, d.Log),
	}
}

// Service exposes the module's service interface (for the CLI, the auth module's
// bootstrapper port, tests, and other wiring).
func (m *Module) Service() Service { return m.service }

// Route returns the ProjectService mount path and handler. opts carries the
// app-wide interceptors (e.g. the auth interceptor).
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewProjectServiceHandler(&handler{svc: m.service}, opts...)
}

// WorkspaceRoute returns the WorkspaceService mount path and handler. The projects
// module owns the workspace aggregate, so it serves both services.
func (m *Module) WorkspaceRoute(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewWorkspaceServiceHandler(&workspaceHandler{svc: m.service}, opts...)
}
