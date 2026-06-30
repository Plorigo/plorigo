package sources

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the sources module needs. Audit, Policy, and Crypto are CONSUMER-DEFINED ports
// (satisfied by *audit.Service, *policy.Service, *crypto.Box), wired in internal/app — sources
// imports none of those modules. Providers is the registry of VCS adapters (built in internal/app
// from the GitHub adapter); all provider access goes through it.
type Deps struct {
	DB        *database.DB
	Audit     Recorder
	Policy    authz.Authorizer
	Crypto    SecretBox
	Providers *Registry
	Log       *slog.Logger
}

// Module is the sources module: the only wiring surface other code touches.
type Module struct {
	service Service
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service: newService(d.DB, store, d.Crypto, d.Providers, d.Policy, d.Audit, d.Log),
	}
}

// Service exposes the module's service (for the connect HTTP handlers in internal/app, the
// deployments/services modules' consumer ports, tests, and wiring).
func (m *Module) Service() Service { return m.service }

// Route returns the SourceService mount path and handler. opts carries the app-wide interceptors.
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewSourceServiceHandler(&handler{svc: m.service}, opts...)
}
