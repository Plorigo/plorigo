package domains

import (
	"log/slog"
	"net"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// Deps are what the domains module needs from platform services and sibling providers.
type Deps struct {
	DB       *database.DB
	Audit    Recorder
	Policy   authz.Authorizer
	Resolver Resolver
	Log      *slog.Logger
}

// Module is the domains module: the only wiring surface other code touches.
type Module struct {
	service Service
}

// New assembles the domains module over its ports.
func New(d Deps) *Module {
	resolver := d.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	store := newPostgresStore(d.DB)
	return &Module{service: newService(d.DB, store, resolver, d.Policy, d.Audit, d.Log)}
}

// Service exposes the module's service interface.
func (m *Module) Service() Service { return m.service }

// Route returns the DomainService mount path and handler.
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewDomainServiceHandler(&handler{svc: m.service}, opts...)
}
