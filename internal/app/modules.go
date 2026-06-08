package app

import (
	"net/http"
	"time"

	"github.com/plorigo/plorigo/internal/audit"
	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/environments"
	"github.com/plorigo/plorigo/internal/membership"
	"github.com/plorigo/plorigo/internal/platform/mailer"
	"github.com/plorigo/plorigo/internal/policy"
	"github.com/plorigo/plorigo/internal/projects"
)

// sessionTTL is how long a browser session (and its cookie) lasts.
const sessionTTL = 720 * time.Hour // 30 days

// buildModules is the SINGLE place that constructs modules and injects cross-module
// interfaces. The construction order encodes the (acyclic) dependency graph:
// membership ← policy ← projects ← auth. Each edge is a consumer-defined port
// satisfied structurally, so no module imports another.
func (a *App) buildModules() {
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
}
