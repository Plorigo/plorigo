package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/principal"
)

// sessionCookieName is the name of the browser session cookie.
const sessionCookieName = "plorigo_session"

// principalResolver turns a session cookie or bearer token into a principal. The
// auth module's Service satisfies it; declared here so the interceptor (which is
// cross-cutting wiring) does not depend on the whole auth surface.
type principalResolver interface {
	ResolveSession(ctx context.Context, sessionToken string) (principal.Principal, error)
	ResolveAPIToken(ctx context.Context, bearer string) (principal.Principal, error)
}

// publicProcedures may be called without authentication. Everything else requires
// a principal; the per-handler authorization (policy.Authorize) is a second gate.
var publicProcedures = map[string]bool{
	"/controlplane.v1.AuthService/Register":             true,
	"/controlplane.v1.AuthService/Login":                true,
	"/controlplane.v1.AuthService/RequestPasswordReset": true,
	"/controlplane.v1.AuthService/ResetPassword":        true,
	"/controlplane.v1.AuthService/VerifyEmail":          true,
}

// authInterceptor resolves the caller's principal from the request, applies a CSRF
// guard to cookie-authenticated browser requests, and rejects non-public procedures
// that have no principal. It never authorizes a specific action — that is the
// service's job via policy.Authorize.
func authInterceptor(resolver principalResolver, dev bool) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			p, viaCookie, err := resolvePrincipal(ctx, resolver, req.Header())
			if err != nil {
				// Fail closed on a backend error rather than leaking it.
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("could not verify credentials"))
			}

			// CSRF (production only; in dev the Vite proxy makes origins awkward and there
			// is no real cross-site risk):
			if !dev {
				// Browsers tag cross-site requests via Sec-Fetch-Site. Reject them for ALL
				// procedures — including the public login/register — so a forged cross-site
				// POST can't act on (or log in) a victim. Non-browser clients (CLI/agent)
				// don't send the header, so they are unaffected.
				if site := req.Header().Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "same-site" && site != "none" {
					return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cross-site request blocked"))
				}
				// Ambient (cookie) authority additionally requires Connect-Web's protocol
				// header, which a cross-site HTML form cannot set without a CORS preflight.
				if viaCookie && p.IsAuthenticated() && req.Header().Get("Connect-Protocol-Version") == "" {
					return nil, connect.NewError(connect.CodePermissionDenied, errors.New("missing Connect-Protocol-Version"))
				}
			}

			ctx = principal.NewContext(ctx, p)
			if !p.IsAuthenticated() && !publicProcedures[req.Spec().Procedure] {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
			}
			return next(ctx, req)
		}
	}
}

// resolvePrincipal reads a bearer token (CLI/agent) or, failing that, the session
// cookie (browser). viaCookie reports which path matched, so the CSRF guard knows
// whether ambient authority is in play.
func resolvePrincipal(ctx context.Context, resolver principalResolver, header http.Header) (principal.Principal, bool, error) {
	if bearer := header.Get("Authorization"); strings.HasPrefix(bearer, "Bearer ") {
		p, err := resolver.ResolveAPIToken(ctx, strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer ")))
		return p, false, err
	}
	r := http.Request{Header: header}
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		p, err := resolver.ResolveSession(ctx, c.Value)
		return p, true, err
	}
	return principal.Principal{}, false, nil
}
