package projects

import (
	"log/slog"
	"net/http"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the projects module needs. Audit is a CONSUMER-DEFINED port,
// satisfied by *audit.Service and wired in internal/app — projects never imports audit.
type Deps struct {
	DB    *database.DB
	Audit Recorder
	Log   *slog.Logger
}

// Module is the projects module: the only wiring surface other code touches.
type Module struct {
	service Service
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service: newService(d.DB, store, d.Audit, d.Log),
	}
}

// Service exposes the module's service interface (for the CLI/tests/other wiring).
func (m *Module) Service() Service { return m.service }

// Route returns the ConnectRPC mount path and handler. Adding a module to the
// control plane = construct it in internal/app and mount its Route().
func (m *Module) Route() (string, http.Handler) {
	return controlplanev1connect.NewProjectServiceHandler(&handler{svc: m.service})
}
