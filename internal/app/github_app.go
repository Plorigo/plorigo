package app

import (
	"net/http"

	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/sources"
)

// appStateCookie holds the sealed GitHub App install state between the install redirect and the
// setup callback (separate from the OAuth state cookie — a workspace can run both flows). It is
// scoped to the App path and short-lived; the callback clears it.
const appStateCookie = "plorigo_gh_app"

// appReturnCookie remembers the dashboard path to land on after the App install completes.
const appReturnCookie = "plorigo_gh_app_return"

// githubAppInstallHandler begins a GitHub App installation: it resolves the browser session, asks
// the sources service for the install URL + sealed state, sets the state cookie, and redirects to
// GitHub's install page. After the user installs, GitHub redirects to the App's configured Setup
// URL (githubAppSetupHandler) with the installation_id.
func (a *App) githubAppInstallHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		returnTo := safeReturnPath(r.URL.Query().Get("return_to"))
		ctx := principal.NewContext(r.Context(), a.resolveBrowserPrincipal(r))
		res, err := a.sources.Service().BeginAppInstall(ctx, sources.BeginConnectInput{
			WorkspaceID: r.URL.Query().Get("workspace_id"),
			Provider:    "github",
		})
		if err != nil {
			a.redirectToDashboard(w, r, returnTo, "error", reasonFor(err))
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     appStateCookie,
			Value:    res.State,
			Path:     "/api/github",
			MaxAge:   600, // 10 minutes, matching the state TTL
			HttpOnly: true,
			Secure:   !a.cfg.Dev,
			SameSite: http.SameSiteLaxMode,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     appReturnCookie,
			Value:    returnTo,
			Path:     "/api/github",
			MaxAge:   600,
			HttpOnly: true,
			Secure:   !a.cfg.Dev,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, res.AuthorizeURL, http.StatusSeeOther)
	})
}

// githubAppSetupHandler is the App's Setup URL: GitHub redirects here after an installation with
// installation_id + setup_action + the echoed state. It verifies the state cookie, stores the
// installation for the workspace, clears the cookie, and redirects back to the dashboard.
func (a *App) githubAppSetupHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieState := ""
		if c, err := r.Cookie(appStateCookie); err == nil {
			cookieState = c.Value
		}
		returnTo := "/projects"
		if c, err := r.Cookie(appReturnCookie); err == nil && c.Value != "" {
			returnTo = safeReturnPath(c.Value)
		}
		// Clear the state and return cookies regardless of outcome — they are single-use.
		http.SetCookie(w, &http.Cookie{Name: appStateCookie, Path: "/api/github", MaxAge: -1, HttpOnly: true, Secure: !a.cfg.Dev, SameSite: http.SameSiteLaxMode})
		http.SetCookie(w, &http.Cookie{Name: appReturnCookie, Path: "/api/github", MaxAge: -1, HttpOnly: true, Secure: !a.cfg.Dev, SameSite: http.SameSiteLaxMode})

		ctx := principal.NewContext(r.Context(), a.resolveBrowserPrincipal(r))
		_, err := a.sources.Service().CompleteAppInstall(ctx, sources.CompleteAppInput{
			Provider:       "github",
			InstallationID: r.URL.Query().Get("installation_id"),
			SetupAction:    r.URL.Query().Get("setup_action"),
			State:          r.URL.Query().Get("state"),
			CookieState:    cookieState,
		})
		if err != nil {
			a.redirectToDashboard(w, r, returnTo, "error", reasonFor(err))
			return
		}
		a.redirectToDashboard(w, r, returnTo, "app_connected", "")
	})
}
