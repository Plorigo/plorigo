package auth

import (
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// CookieConfig controls the session cookie. The app derives Secure from whether it
// serves over HTTPS (production); SameSite is Lax so top-level navigations work
// while cross-site POSTs are blocked.
type CookieConfig struct {
	Name          string
	Secure        bool
	SameSite      http.SameSite
	MaxAgeSeconds int
}

// Deps are what the auth module needs. Audit, Mailer, and Workspace are
// CONSUMER-DEFINED ports satisfied by *audit.Service, the platform mailer, and
// *projects.Service, wired in internal/app — auth imports none of those modules.
type Deps struct {
	Cfg       Config
	Cookie    CookieConfig
	DB        *database.DB
	Audit     Recorder
	Mailer    Mailer
	Workspace WorkspaceBootstrapper
	Log       *slog.Logger
}

// Module is the auth module: the only wiring surface other code touches.
type Module struct {
	service Service
	cookie  CookieConfig
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service: newService(d.Cfg, d.DB, store, d.Audit, d.Mailer, d.Workspace, d.Log),
		cookie:  d.Cookie,
	}
}

// Service exposes the auth service — used by the app's auth interceptor (for the
// session/token resolvers) and by tests.
func (m *Module) Service() Service { return m.service }

// Route returns the AuthService mount path and handler. opts carries the app-wide
// interceptors (notably the auth interceptor); see internal/app.
func (m *Module) Route(opts ...connect.HandlerOption) (string, http.Handler) {
	return controlplanev1connect.NewAuthServiceHandler(&handler{svc: m.service, cookie: m.cookie}, opts...)
}
