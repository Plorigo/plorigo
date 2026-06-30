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
	mux.Handle(a.config.Route(ic))
	mux.Handle(a.servers.Route(ic))
	mux.Handle(a.serversetup.Route(ic))
	mux.Handle(a.agents.Route(ic))
	mux.Handle(a.deployments.Route(ic))
	mux.Handle(a.domains.Route(ic))
	mux.Handle(a.sources.Route(ic))
	mux.Handle(a.services.Route(ic))
	mux.Handle(a.backups.Route(ic))
	mux.Handle(a.readiness.Route(ic))

	// GitHub OAuth is a browser redirect flow, not ConnectRPC: these endpoints set a
	// state cookie and 302, so they are plain HTTP handlers (outside the interceptor)
	// that resolve the session themselves. See github_oauth.go.
	mux.Handle("GET /api/github/connect", a.githubConnectHandler())
	mux.Handle("GET /api/github/callback", a.githubCallbackHandler())
	// GitHub App installation: begin redirects to GitHub's install page; the setup URL is where
	// GitHub returns after an install (it carries installation_id). Both are plain HTTP (cookies +
	// 302), like the OAuth flow. See github_app.go and docs/architecture/security.md.
	mux.Handle("GET /api/github/app/install", a.githubAppInstallHandler())
	mux.Handle("GET /api/github/app/setup", a.githubAppSetupHandler())
	// Automated GitHub App registration (the manifest flow): /new authorizes + returns an
	// auto-submitting form that POSTs the manifest to GitHub; GitHub redirects to /manifest/callback
	// with a temporary code the control plane exchanges for the App's credentials. See
	// github_app_manifest.go and docs/architecture/sources.md.
	mux.Handle("GET /api/github/app/manifest/new", a.githubAppManifestNewHandler())
	mux.Handle("GET /api/github/app/manifest/callback", a.githubAppManifestCallbackHandler())
	// Inbound GitHub App webhooks: the handler verifies the HMAC signature over the raw body before
	// acting, so it is plain HTTP (outside the auth interceptor). See github_webhook.go.
	mux.Handle("POST /api/github/webhook", a.githubWebhookHandler())
	// The agent gateways: agent.v1 procedures are public (see auth_interceptor.go); the
	// services validate the registration token / agent credential in the request body.
	mux.Handle(a.agents.AgentRoute(ic))
	mux.Handle(a.deployments.AgentRoute(ic))
	mux.Handle(a.deployments.TeardownAgentRoute(ic))
	mux.Handle(a.backups.AgentRoute(ic))

	// Dashboard / SPA fallback.
	mux.Handle("/", web.Handler())

	return mux
}
