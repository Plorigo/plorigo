package webhooks

import (
	"log/slog"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Deps are what the webhooks module needs. Creator and Teardowner are CONSUMER-DEFINED ports
// (satisfied structurally by *deployments.Service), wired in internal/app — webhooks imports no
// other module. It is a provider-only module: it has no ConnectRPC surface (the inbound HTTP
// endpoint lives in internal/app and verifies the signature before calling Service).
type Deps struct {
	DB         *database.DB
	Creator    PreviewCreator
	Teardowner PreviewTeardowner
	Log        *slog.Logger
}

// Module is the webhooks module: it exposes its Service for the HTTP webhook handler in internal/app.
type Module struct {
	service Service
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	return &Module{service: newService(newPostgresStore(d.DB), d.Creator, d.Teardowner, d.Log)}
}

// Service exposes the module's service interface (for the HTTP webhook handler in internal/app).
func (m *Module) Service() Service { return m.service }
