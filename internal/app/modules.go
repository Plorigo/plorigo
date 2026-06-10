package app

import (
	"net/http"
	"time"

	"github.com/plorigo/plorigo/internal/agents"
	"github.com/plorigo/plorigo/internal/audit"
	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/deployments"
	"github.com/plorigo/plorigo/internal/environments"
	"github.com/plorigo/plorigo/internal/envvars"
	"github.com/plorigo/plorigo/internal/membership"
	"github.com/plorigo/plorigo/internal/platform/crypto"
	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/mailer"
	"github.com/plorigo/plorigo/internal/policy"
	"github.com/plorigo/plorigo/internal/projects"
	"github.com/plorigo/plorigo/internal/secrets"
	"github.com/plorigo/plorigo/internal/servers"
	"github.com/plorigo/plorigo/internal/sources"
)

// sessionTTL is how long a browser session (and its cookie) lasts.
const sessionTTL = 720 * time.Hour // 30 days

// buildModules is the SINGLE place that constructs modules and injects cross-module
// interfaces. The construction order encodes the (acyclic) dependency graph:
// membership ← policy ← projects ← auth. Each edge is a consumer-defined port
// satisfied structurally, so no module imports another. It returns an error when a
// dependency cannot be built (e.g. an invalid APP_MASTER_KEY), so the control plane
// fails fast at startup rather than at first use.
func (a *App) buildModules() error {
	auditSvc := audit.New(audit.Deps{Log: a.log})

	// membership (role reader) → policy (decisions) → projects (privileged writes).
	membershipSvc := membership.New(membership.Deps{DB: a.db, Log: a.log})
	policySvc := policy.New(policy.Deps{Membership: membershipSvc, Log: a.log})

	a.projects = projects.New(projects.Deps{
		DB:    a.db,
		Audit: auditSvc,
		// *policy.Service satisfies projects' authz.Authorizer port.
		Policy: policySvc,
		Log:    a.log,
	})

	// environments are project-scoped; they authorize/audit against the workspace
	// resolved through the parent project (no dependency on the projects module).
	a.environments = environments.New(environments.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Log:    a.log,
	})

	// env vars are non-secret, per-environment config; like environments they
	// authorize/audit against the workspace resolved through environment -> project.
	a.envvars = envvars.New(envvars.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Log:    a.log,
	})

	// secrets are the encrypted, write-only counterpart to env vars: same
	// environment-scoping, but values are sealed at rest by the crypto box (keyed by
	// APP_MASTER_KEY). A bad master key fails here, before the server starts.
	box, err := crypto.NewBox(a.cfg.MasterKey)
	if err != nil {
		return err
	}
	a.secrets = secrets.New(secrets.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Crypto: box,
		Log:    a.log,
	})

	// servers are workspace-scoped (like projects): a connected machine a workspace
	// deploys onto, authorized/audited directly against the workspace.
	a.servers = servers.New(servers.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Log:    a.log,
	})

	// agents are the control-plane side of the server agent: registration tokens,
	// keys, and liveness. Server-scoped — the owning workspace is resolved from the
	// server, then authorized/audited like servers. PublicURL + Dev shape the install
	// command (dev runs the agent from the local checkout).
	a.agents = agents.New(agents.Deps{
		DB:        a.db,
		Audit:     auditSvc,
		Policy:    policySvc,
		PublicURL: a.cfg.PublicURL,
		Dev:       a.cfg.Dev,
		Log:       a.log,
	})

	// deployments record an attempt to run an image in an environment on a server and
	// dispatch it to that server's agent. Environment-scoped like env vars (workspace
	// resolved through environment -> project); also serves the agent-facing
	// DeployService gateway (claim/report), public like the agent registration gateway.
	a.deployments = deployments.New(deployments.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Log:    a.log,
	})

	// sources connect a project to a GitHub repository via the workspace's OAuth
	// connection. The OAuth token is sealed by the same crypto box as secrets (reused
	// here); the GitHub client is the outbound adapter (*github.Client satisfies the
	// module's GitHubClient port). The OAuth callback URL is derived from PublicURL.
	a.sources = sources.New(sources.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Crypto: box,
		GitHub: github.NewClient(github.Config{}),
		OAuth: sources.OAuthConfig{
			ClientID:     a.cfg.GitHubClientID,
			ClientSecret: a.cfg.GitHubClientSecret,
			Scopes:       a.cfg.GitHubScopes,
			RedirectURL:  a.cfg.GitHubRedirectURL(),
		},
		Log: a.log,
	})

	mailerSvc := mailer.New(mailer.Config{
		SMTPHost: a.cfg.SMTPHost,
		SMTPPort: a.cfg.SMTPPort,
		Username: a.cfg.SMTPUser,
		Password: a.cfg.SMTPPass,
		From:     a.cfg.EmailFrom,
	}, a.log)

	a.auth = auth.New(auth.Deps{
		Cfg: auth.Config{
			BaseURL:                  a.cfg.BaseURL,
			SessionTTL:               sessionTTL,
			AllowOpenRegistration:    a.cfg.AllowOpenRegistration,
			RequireEmailVerification: a.cfg.RequireEmailVerification,
		},
		Cookie: auth.CookieConfig{
			Name:          sessionCookieName,
			Secure:        !a.cfg.Dev, // require HTTPS in production; off for http://localhost
			SameSite:      http.SameSiteLaxMode,
			MaxAgeSeconds: int(sessionTTL.Seconds()),
		},
		DB:     a.db,
		Audit:  auditSvc,
		Mailer: mailerSvc,
		// *projects.Service satisfies auth's WorkspaceBootstrapper port. This is the
		// only edge auth → projects, used to create the new user's first workspace.
		Workspace: a.projects.Service(),
		Log:       a.log,
	})

	return nil
}
