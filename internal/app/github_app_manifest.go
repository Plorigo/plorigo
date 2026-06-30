package app

import (
	"html/template"
	"net/http"

	"github.com/plorigo/plorigo/internal/githubapp"
	"github.com/plorigo/plorigo/internal/platform/principal"
)

// manifestStateCookie holds the sealed manifest-registration state between the form POST to GitHub
// and the callback. manifestReturnCookie remembers where to land in the dashboard afterward. Both
// are scoped to the App path, short-lived, and cleared by the callback.
const (
	manifestStateCookie  = "plorigo_gh_manifest"
	manifestReturnCookie = "plorigo_gh_manifest_return"
)

// manifestFormTmpl is an auto-submitting HTML form that POSTs the App manifest to GitHub's
// create-from-manifest page (a GET redirect can't carry the manifest body). html/template escapes
// the action URL and the manifest JSON for their respective attribute contexts.
var manifestFormTmpl = template.Must(template.New("manifest").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Registering GitHub App…</title></head>
<body onload="document.forms[0].submit()">
<form action="{{.Action}}" method="post">
<input type="hidden" name="manifest" value="{{.Manifest}}">
<noscript><p>Continue to GitHub to create the app:</p><button type="submit">Create GitHub App</button></noscript>
</form>
</body></html>`))

// githubAppManifestNewHandler begins automated GitHub App registration: it resolves the browser
// session, asks githubapp to build the manifest + sealed state (authorizing the caller as a
// workspace owner), sets the state + return cookies, and returns an auto-submitting form that POSTs
// the manifest to GitHub. GitHub then redirects to the manifest's redirect_url (the callback below).
func (a *App) githubAppManifestNewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		returnTo := safeReturnPath(r.URL.Query().Get("return_to"))
		ctx := principal.NewContext(r.Context(), a.resolveBrowserPrincipal(r))
		res, err := a.githubapp.BeginRegistration(ctx, githubapp.BeginRegistrationInput{
			WorkspaceID: r.URL.Query().Get("workspace_id"),
			Org:         r.URL.Query().Get("org"),
		})
		if err != nil {
			a.redirectToDashboard(w, r, returnTo, "error", reasonFor(err))
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     manifestStateCookie,
			Value:    res.State,
			Path:     "/api/github",
			MaxAge:   600, // 10 minutes, matching the state TTL
			HttpOnly: true,
			Secure:   !a.cfg.Dev,
			SameSite: http.SameSiteLaxMode,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     manifestReturnCookie,
			Value:    returnTo,
			Path:     "/api/github",
			MaxAge:   600,
			HttpOnly: true,
			Secure:   !a.cfg.Dev,
			SameSite: http.SameSiteLaxMode,
		})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The page only redirects the browser to GitHub; no app data is rendered, but escape anyway.
		if err := manifestFormTmpl.Execute(w, struct{ Action, Manifest string }{Action: res.FormAction, Manifest: res.ManifestJSON}); err != nil {
			a.redirectToDashboard(w, r, returnTo, "error", "could not start app registration")
		}
	})
}

// githubAppManifestCallbackHandler is the manifest redirect_url: GitHub redirects here with a
// temporary code + the echoed state. It verifies the sealed state cookie, exchanges the code for the
// new App's credentials (stored sealed), clears the cookies, and returns to the dashboard.
func (a *App) githubAppManifestCallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieState := ""
		if c, err := r.Cookie(manifestStateCookie); err == nil {
			cookieState = c.Value
		}
		returnTo := "/integrations"
		if c, err := r.Cookie(manifestReturnCookie); err == nil && c.Value != "" {
			returnTo = safeReturnPath(c.Value)
		}
		// Clear the single-use cookies regardless of outcome.
		http.SetCookie(w, &http.Cookie{Name: manifestStateCookie, Path: "/api/github", MaxAge: -1, HttpOnly: true, Secure: !a.cfg.Dev, SameSite: http.SameSiteLaxMode})
		http.SetCookie(w, &http.Cookie{Name: manifestReturnCookie, Path: "/api/github", MaxAge: -1, HttpOnly: true, Secure: !a.cfg.Dev, SameSite: http.SameSiteLaxMode})

		ctx := principal.NewContext(r.Context(), a.resolveBrowserPrincipal(r))
		_, err := a.githubapp.CompleteRegistration(ctx, githubapp.CompleteRegistrationInput{
			Code:        r.URL.Query().Get("code"),
			State:       r.URL.Query().Get("state"),
			CookieState: cookieState,
		})
		if err != nil {
			a.redirectToDashboard(w, r, returnTo, "error", reasonFor(err))
			return
		}
		a.redirectToDashboard(w, r, returnTo, "app_registered", "")
	})
}
