package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/plorigo/plorigo/internal/agents"
	"github.com/plorigo/plorigo/internal/audit"
	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/backups"
	"github.com/plorigo/plorigo/internal/config"
	"github.com/plorigo/plorigo/internal/deployments"
	"github.com/plorigo/plorigo/internal/domains"
	"github.com/plorigo/plorigo/internal/environments"
	"github.com/plorigo/plorigo/internal/githubapp"
	"github.com/plorigo/plorigo/internal/membership"
	"github.com/plorigo/plorigo/internal/platform/crypto"
	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/mailer"
	"github.com/plorigo/plorigo/internal/policy"
	"github.com/plorigo/plorigo/internal/projects"
	"github.com/plorigo/plorigo/internal/readiness"
	"github.com/plorigo/plorigo/internal/servers"
	"github.com/plorigo/plorigo/internal/serversetup"
	"github.com/plorigo/plorigo/internal/services"
	"github.com/plorigo/plorigo/internal/sources"
	"github.com/plorigo/plorigo/internal/webhooks"
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

	// The crypto box seals secret values at rest (AES-256-GCM, keyed by APP_MASTER_KEY) and
	// opens them at deploy time. A bad master key fails here, before the server starts. It is
	// reused by config (seal), deployments (open), and sources/services (OAuth token sealing).
	box, err := crypto.NewBox(a.cfg.MasterKey)
	if err != nil {
		return err
	}

	// config is unified configuration: variables (plaintext, readable) and secrets
	// (encrypted, write-only) at service or environment scope. It authorizes/audits against
	// the workspace resolved through the service or the environment's project, and seals
	// secret values with the crypto box.
	a.config = config.New(config.Deps{
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

	// serversetup owns the persistent SSH management credential for a server (the
	// dashboard-managed setup/repair channel). The private key is sealed at rest by the
	// same crypto box as secrets (reused here) and is write-only; the module governs its
	// lifecycle (provision/rotate/revoke), authorized/audited against the server's
	// workspace. The actual SSH bootstrap runner is built on top of this later.
	a.serversetup = serversetup.New(serversetup.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Crypto: box,
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

	// serversetup owns the dashboard-managed SSH setup run AND the persistent management
	// credential it provisions. The private key is sealed by the same crypto box as secrets;
	// the run drives the shared installer over SSH (via its own dialer) and waits on the agent
	// through an adapter over the agents module (so serversetup never imports it). Built after
	// agents because it depends on them.
	a.serversetup = serversetup.New(serversetup.Deps{
		DB:        a.db,
		Audit:     auditSvc,
		Policy:    policySvc,
		Crypto:    box,
		Log:       a.log,
		Dialer:    serversetup.NewSSHDialer(),
		Agents:    agentSetupAdapter{agents: a.agents.Service(), now: time.Now},
		PublicURL: a.cfg.PublicURL,
	})

	// githubapp owns the instance's server-wide GitHub App credentials. They come from the
	// GITHUB_APP_* env vars (operator-set, precedence) or — when those are unset — a sealed
	// singleton row written by the dashboard's automated manifest-registration flow. It exposes
	// resolvers the github client (app id + key), the webhook handler (secret), and the sources
	// module (slug/configured + install URL) consume, so a runtime-registered App takes effect with
	// no restart. Its own plain github client is only for the no-auth manifest conversion call.
	// Built before sources + the App-aware client, which read credentials through it.
	a.githubapp = githubapp.New(githubapp.Deps{
		DB:     a.db,
		Crypto: box,
		GitHub: github.NewClient(github.Config{}),
		Policy: policySvc,
		Audit:  auditSvc,
		Env: githubapp.EnvConfig{
			AppID:         a.cfg.GitHubAppID,
			PrivateKeyPEM: a.cfg.GitHubAppPrivateKey,
			Slug:          a.cfg.GitHubAppSlug,
			WebhookSecret: a.cfg.GitHubWebhookSecret,
		},
		BaseURL:    a.cfg.BaseURL,
		WebhookURL: strings.TrimRight(a.cfg.BaseURL, "/") + "/api/github/webhook",
		Log:        a.log,
	}).Service()

	// The App-aware GitHub client resolves its App id + private key dynamically through githubapp
	// (env or the registered App), so installation tokens are minted for private builds without a
	// restart after registration. The GitHub provider ADAPTER wraps it + the OAuth config + the App
	// config resolver, and is the only GitHub-specific piece registered in the provider registry —
	// adding GitLab later means registering another adapter, not changing sources.
	githubClient := github.NewClient(github.Config{AppCredentials: a.githubapp.AppCredentials})
	githubProvider := sources.NewGitHub(githubClient, sources.OAuthConfig{
		ClientID:     a.cfg.GitHubClientID,
		ClientSecret: a.cfg.GitHubClientSecret,
		Scopes:       a.cfg.GitHubScopes,
		RedirectURL:  a.cfg.GitHubRedirectURL(),
	}, a.githubapp)
	providerRegistry := sources.NewRegistry(githubProvider)

	// sources owns a workspace's integrations (connections) across providers, going through the
	// registry for all VCS access. The OAuth token is sealed by the same crypto box as secrets. Built
	// before deployments + services, which reach providers only through it.
	a.sources = sources.New(sources.Deps{
		DB:        a.db,
		Audit:     auditSvc,
		Policy:    policySvc,
		Crypto:    box,
		Providers: providerRegistry,
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
		// Decrypts environment/service secrets at deploy time so their plaintext can be
		// injected into the container (the same box that config seals them with).
		Crypto: box,
		// Resolves a pull request to its head ref + URL when creating a PR preview
		// (*github.Client satisfies the consumer-defined port).
		GitHub: github.NewClient(github.Config{}),
		// Mints a short-lived installation token for a private (GitHub App-backed) build at claim
		// time (sources.Service satisfies the consumer-defined SourceTokens port).
		Sources: a.sources.Service(),
		Log:     a.log,
	})

	// domains attach one or more custom hostnames to a service. They authorize through the
	// owning service's denormalized workspace and use DNS lookups for explicit verification.
	a.domains = domains.New(domains.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Log:    a.log,
	})

	// services are a project's deployable components, each living in one environment and
	// owning its source (folded onto the row), port, visibility, env vars, and deployment
	// history. CreateService validates a git source through the same GitHub client as
	// sources and seals/opens through the same crypto box; deploy_now enqueues the first
	// deployment through the deployments Enqueuer port (*deployments.Service) — built above,
	// so this is constructed after it.
	a.services = services.New(services.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Crypto: box,
		// Public repos validate anonymously through this client; connected (oauth/app) repos validate
		// through the sources module's provider seam.
		GitHub:   github.NewClient(github.Config{}),
		Sources:  sourcesForServices{svc: a.sources.Service()},
		Enqueuer: a.deployments.Service(),
		Config:   a.config.Service(),
		Log:      a.log,
	})

	// Backups capture a managed Postgres service's data via the database's server agent. It needs
	// no Crypto (managed-DB credentials are plaintext config variables, not sealed secrets) and
	// resolves the target service + running server through sibling reads in its own postgres.go.
	a.backups = backups.New(backups.Deps{
		DB:     a.db,
		Audit:  auditSvc,
		Policy: policySvc,
		Log:    a.log,
	})

	// webhooks turns verified inbound GitHub deliveries into preview actions. It drives the
	// deployments service through consumer-defined ports (*deployments.Service satisfies the
	// PreviewCreator + PreviewTeardowner ports structurally) — built after deployments. The HTTP
	// endpoint that verifies the signature and calls it is github_webhook.go.
	a.webhooks = webhooks.New(webhooks.Deps{
		DB:         a.db,
		Creator:    a.deployments.Service(),
		Teardowner: a.deployments.Service(),
		Log:        a.log,
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

	// The Production Readiness Doctor reads (never writes) state from services, config, domains,
	// deployments, and agents through consumer-defined ports (the readiness*Reader adapters), so
	// it is built last — after every module it reads from. Backups is nil until that module
	// exists; the backup check degrades to "not available yet". Server readiness is derived the
	// same way the dashboard derives it (time.Now + Dev relaxes the Linux-only host check).
	a.readiness = readiness.New(readiness.Deps{
		Services:    readinessServiceReader{services: a.services.Service()},
		Config:      readinessConfigReader{config: a.config.Service()},
		Domains:     readinessDomainReader{domains: a.domains.Service()},
		Deployments: readinessDeploymentReader{deployments: a.deployments.Service()},
		Servers:     readinessServerReader{agents: a.agents.Service(), now: time.Now, dev: a.cfg.Dev},
		Backups:     nil,
		Policy:      policySvc,
		Log:         a.log,
	})

	return nil
}
