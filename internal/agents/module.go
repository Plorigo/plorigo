package agents

import (
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the agents module needs. Audit and Policy are CONSUMER-DEFINED ports
// (authz.Authorizer is satisfied by *policy.Service, Recorder by *audit.Service), wired
// in internal/app — agents imports neither module. PublicURL is the control plane's
// public URL (the RPC endpoint the agent connects to), used to render the install
// command; Dev switches that command to run the agent from the local source checkout.
type Deps struct {
	DB        *database.DB
	Audit     Recorder
	Policy    authz.Authorizer
	PublicURL string
	Dev       bool
	Log       *slog.Logger
}

// Module is the agents module: the only wiring surface other code touches.
type Module struct {
	service   Service
	publicURL string
	dev       bool
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service:   newService(d.DB, store, d.Policy, d.Audit, d.Log),
		publicURL: d.PublicURL,
		dev:       d.Dev,
	}
}

// Service exposes the module's service interface (for internal/app, tests, and other
// wiring).
func (m *Module) Service() Service { return m.service }

// Route returns the dashboard-facing controlplane.v1.AgentService mount and handler.
// opts carries the app-wide interceptors (e.g. the auth interceptor).
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewAgentServiceHandler(&adminHandler{svc: m.service, publicURL: m.publicURL, dev: m.dev, now: time.Now}, opts...)
}

// AgentRoute returns the agent-facing agent.v1.AgentService mount and handler. Its
// procedures are public at the auth interceptor; the service validates the registration
// token / credential carried in the request body.
func (m *Module) AgentRoute(opts ...connect.HandlerOption) (string, http.Handler) {
	return agentv1connect.NewAgentServiceHandler(&gatewayHandler{svc: m.service}, opts...)
}
