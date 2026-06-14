package services

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the services module needs. Audit, Policy, Crypto, GitHub, and Enqueuer are
// CONSUMER-DEFINED ports (authz.Authorizer is satisfied by *policy.Service, Recorder by
// *audit.Service, SecretBox by *crypto.Box, GitHubClient by *github.Client, Enqueuer by
// *deployments.Service), wired in internal/app — services imports none of those modules.
type Deps struct {
	DB       *database.DB
	Audit    Recorder
	Policy   authz.Authorizer
	Crypto   SecretBox
	GitHub   GitHubClient
	Enqueuer Enqueuer
	Log      *slog.Logger
}

// Module is the services module: the only wiring surface other code touches.
type Module struct {
	service Servicer
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service: newService(d.DB, store, d.Crypto, d.GitHub, d.Enqueuer, d.Policy, d.Audit, d.Log),
	}
}

// Service exposes the module's service interface (for tests and other wiring).
func (m *Module) Service() Servicer { return m.service }

// Route returns the ServiceService mount path and handler. opts carries the app-wide
// interceptors (e.g. the auth interceptor).
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewServiceServiceHandler(&handler{svc: m.service}, opts...)
}
