package app

import (
	"net/http"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/web"
)

// router builds the mux served over h2c: each module's ConnectRPC route (wrapped by
// the auth interceptor), plus the dashboard as the fallback for everything else.
func (a *App) router() http.Handler {
	mux := http.NewServeMux()

	// One interceptor wraps every RPC: it resolves the caller's principal from the
	// session cookie or bearer token and enforces authentication for non-public
	// procedures. Per-action authorization happens inside the services.
	ic := connect.WithInterceptors(authInterceptor(a.auth.Service(), a.cfg.Dev))

	mux.Handle(a.auth.Route(ic))
	mux.Handle(a.projects.Route(ic))
	mux.Handle(a.projects.WorkspaceRoute(ic))
	mux.Handle(a.environments.Route(ic))
	mux.Handle(a.envvars.Route(ic))
	mux.Handle(a.secrets.Route(ic))
	mux.Handle(a.servers.Route(ic))

	// Dashboard / SPA fallback.
	mux.Handle("/", web.Handler())

	return mux
}
