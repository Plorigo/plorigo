package deployments

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the deployments module needs. Audit and Policy are CONSUMER-DEFINED
// ports (authz.Authorizer is satisfied by *policy.Service, Recorder by *audit.Service),
// wired in internal/app — deployments imports neither module.
type Deps struct {
	DB     *database.DB
	Audit  Recorder
	Policy authz.Authorizer
	// Crypto decrypts environment/service secrets at deploy time so their plaintext can be
	// injected into the container. Satisfied by *crypto.Box (same box that seals them).
	Crypto Opener
	// GitHub resolves a pull request to its head ref + URL when creating a PR preview.
	// Satisfied by *github.Client (the same client the sources module uses).
	GitHub GitHubClient
	Log    *slog.Logger
}

// Module is the deployments module: the only wiring surface other code touches. It
// serves the dashboard-facing DeploymentService and the agent-facing DeployService.
type Module struct {
	service Service
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service: newService(d.DB, store, d.Policy, d.Audit, d.Crypto, d.GitHub, d.Log),
	}
}

// Service exposes the module's service interface (for internal/app, tests, and other
// wiring).
func (m *Module) Service() Service { return m.service }

// Route returns the dashboard-facing controlplane.v1.DeploymentService mount and
// handler. opts carries the app-wide interceptors (e.g. the auth interceptor).
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewDeploymentServiceHandler(&adminHandler{svc: m.service}, opts...)
}

// AgentRoute returns the agent-facing agent.v1.DeployService mount and handler. Its
// procedures are public at the auth interceptor; the service validates the agent
// credential carried in the request body.
func (m *Module) AgentRoute(opts ...connect.HandlerOption) (string, http.Handler) {
	return agentv1connect.NewDeployServiceHandler(&gatewayHandler{svc: m.service}, opts...)
}

// TeardownAgentRoute returns the agent-facing agent.v1.TeardownService mount and handler (preview
// teardown claim/report). Public at the auth interceptor; the service validates the agent
// credential in the request body, like AgentRoute.
func (m *Module) TeardownAgentRoute(opts ...connect.HandlerOption) (string, http.Handler) {
	return agentv1connect.NewTeardownServiceHandler(&teardownGatewayHandler{svc: m.service}, opts...)
}
