package app

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
	"github.com/plorigo/plorigo/internal/sources"
)

// oauthStateCookie holds the sealed OAuth state between the connect redirect and the
// callback. It is scoped to the OAuth path and short-lived; the callback clears it.
const oauthStateCookie = "plorigo_gh_oauth"

// oauthReturnCookie remembers the dashboard path to land on after the flow, so a
// connect started from the launchpad (/projects/new) returns there rather than the
// default projects list. It rides next to the state cookie and is cleared with it.
const oauthReturnCookie = "plorigo_gh_return"

// githubConnectHandler begins the GitHub OAuth flow: it resolves the browser session,
// asks the sources service for an authorize URL + sealed state, sets the state cookie,
// and redirects to GitHub. On any error it bounces back to the dashboard with a reason.
func (a *App) githubConnectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		returnTo := safeReturnPath(r.URL.Query().Get("return_to"))
		ctx := principal.NewContext(r.Context(), a.resolveBrowserPrincipal(r))
		res, err := a.sources.Service().BeginOAuth(ctx, sources.BeginConnectInput{
			WorkspaceID: r.URL.Query().Get("workspace_id"),
			Provider:    "github",
		})
		if err != nil {
			a.redirectToDashboard(w, r, returnTo, "error", reasonFor(err))
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     oauthStateCookie,
			Value:    res.State,
			Path:     "/api/github",
			MaxAge:   600, // 10 minutes, matching the state TTL
			HttpOnly: true,
			Secure:   !a.cfg.Dev,
			SameSite: http.SameSiteLaxMode,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     oauthReturnCookie,
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

// githubCallbackHandler completes the flow: it verifies the state cookie against the
// echoed state, exchanges the code for a token (stored sealed), clears the cookie, and
// redirects back to the dashboard.
func (a *App) githubCallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieState := ""
		if c, err := r.Cookie(oauthStateCookie); err == nil {
			cookieState = c.Value
		}
		returnTo := "/projects"
		if c, err := r.Cookie(oauthReturnCookie); err == nil && c.Value != "" {
			returnTo = safeReturnPath(c.Value)
		}
		// Clear the state and return cookies regardless of outcome — they are single-use.
		http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Path: "/api/github", MaxAge: -1, HttpOnly: true, Secure: !a.cfg.Dev, SameSite: http.SameSiteLaxMode})
		http.SetCookie(w, &http.Cookie{Name: oauthReturnCookie, Path: "/api/github", MaxAge: -1, HttpOnly: true, Secure: !a.cfg.Dev, SameSite: http.SameSiteLaxMode})

		ctx := principal.NewContext(r.Context(), a.resolveBrowserPrincipal(r))
		_, err := a.sources.Service().CompleteOAuth(ctx, sources.CompleteOAuthInput{
			Provider:    "github",
			Code:        r.URL.Query().Get("code"),
			State:       r.URL.Query().Get("state"),
			CookieState: cookieState,
		})
		if err != nil {
			a.redirectToDashboard(w, r, returnTo, "error", reasonFor(err))
			return
		}
		a.redirectToDashboard(w, r, returnTo, "connected", "")
	})
}

// resolveBrowserPrincipal reads the session cookie and resolves it to a principal,
// returning the zero (unauthenticated) principal when there is no valid session. The
// per-action authorization still happens inside the sources service.
func (a *App) resolveBrowserPrincipal(r *http.Request) principal.Principal {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return principal.Principal{}
	}
	p, err := a.auth.Service().ResolveSession(r.Context(), c.Value)
	if err != nil {
		return principal.Principal{}
	}
	return p
}

// redirectToDashboard sends the browser back to a dashboard path with a github status
// (and optional reason) the dashboard surfaces as a toast. returnPath is a sanitized
// same-origin path (see safeReturnPath).
func (a *App) redirectToDashboard(w http.ResponseWriter, r *http.Request, returnPath, status, reason string) {
	q := url.Values{}
	q.Set("github", status)
	if reason != "" {
		q.Set("reason", reason)
	}
	target := strings.TrimRight(a.cfg.BaseURL, "/") + returnPath + "?" + q.Encode()
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// safeReturnPath restricts the post-OAuth landing to a same-origin dashboard path,
// defaulting to /projects. It blocks absolute and protocol-relative URLs so the
// return_to parameter can never be used as an open redirect, and drops any query or
// fragment a caller tries to smuggle in (the status params are added by the caller).
func safeReturnPath(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/projects"
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// reasonFor extracts a safe, user-facing message from a domain error for the redirect.
func reasonFor(err error) string {
	var pe *problem.Error
	if errors.As(err, &pe) {
		return pe.Message
	}
	return "could not connect to GitHub"
}
