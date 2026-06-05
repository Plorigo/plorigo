package app

import (
	"net/http"

	"github.com/plorigo/plorigo/internal/platform/web"
)

// router builds the mux served over h2c: each module's ConnectRPC route, plus the
// dashboard as the fallback for everything else.
func (a *App) router() http.Handler {
	mux := http.NewServeMux()

	// Module RPC routes. Each module returns its (path, handler) from Route().
	mux.Handle(a.projects.Route())

	// Dashboard / SPA fallback.
	mux.Handle("/", web.Handler())

	return mux
}
