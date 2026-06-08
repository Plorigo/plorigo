package environments

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the environments module needs. Audit and Policy are CONSUMER-DEFINED
// ports (authz.Authorizer is satisfied by *policy.Service, Recorder by
// *audit.Service), wired in internal/app — environments imports neither module.
type Deps struct {
	DB     *database.DB
	Audit  Recorder
	Policy authz.Authorizer
	Log    *slog.Logger
}

// Module is the environments module: the only wiring surface other code touches.
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

// Service exposes the module's service interface (for internal/app, tests, and
// other wiring).
func (m *Module) Service() Service { return m.service }

// Route returns the EnvironmentService mount path and handler. opts carries the
// app-wide interceptors (e.g. the auth interceptor).
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewEnvironmentServiceHandler(&handler{svc: m.service}, opts...)
}
