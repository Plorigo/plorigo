package backups

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the backups module needs. Audit and Policy are CONSUMER-DEFINED ports
// (authz.Authorizer is satisfied by *policy.Service, Recorder by *audit.Service), wired in
// internal/app — backups imports neither module. It needs no Crypto: managed-database
// credentials are plaintext config variables, not sealed secrets.
type Deps struct {
	DB     *database.DB
	Audit  Recorder
	Policy authz.Authorizer
	Log    *slog.Logger
}

// Module is the backups module: it serves the dashboard-facing controlplane.v1.BackupService and
// the agent-facing agent.v1.BackupService.
type Module struct {
	service Service
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{service: newService(d.DB, store, d.Policy, d.Audit, d.Log)}
}

// Service exposes the module's service interface.
func (m *Module) Service() Service { return m.service }

// Route returns the dashboard-facing controlplane.v1.BackupService mount and handler.
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewBackupServiceHandler(&adminHandler{svc: m.service}, opts...)
}

// AgentRoute returns the agent-facing agent.v1.BackupService mount and handler. Its procedures
// are public at the auth interceptor; the service validates the agent credential in the body.
func (m *Module) AgentRoute(opts ...connect.HandlerOption) (string, http.Handler) {
	return agentv1connect.NewBackupServiceHandler(&gatewayHandler{svc: m.service}, opts...)
}
