package readiness

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are the read ports the readiness module reads through, plus the authorizer. They are
// consumer-defined interfaces satisfied structurally by adapters in internal/app, so this module
// imports no sibling module. Backups is OPTIONAL (nil until the backups module exists).
type Deps struct {
	Services    ServiceReader
	Config      ConfigReader
	Domains     DomainReader
	Deployments DeploymentReader
	Servers     ServerReader
	Backups     BackupReader
	Policy      authz.Authorizer
	Log         *slog.Logger
}

// Module is the readiness module: the only wiring surface other code touches.
type Module struct {
	service Service
}

// New assembles the readiness module over its read ports. It owns no database.
func New(d Deps) *Module {
	return &Module{service: newService(d.Services, d.Config, d.Domains, d.Deployments, d.Servers, d.Backups, d.Policy, d.Log)}
}

// Service exposes the module's service interface.
func (m *Module) Service() Service { return m.service }

// Route returns the ReadinessService mount path and handler.
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewReadinessServiceHandler(&handler{svc: m.service}, opts...)
}
