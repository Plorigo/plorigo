package githubapp

import (
	"context"
	"log/slog"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
)

// Service is the githubapp module's surface. The first four methods are resolvers consumed by the
// github client, the webhook handler, and the sources module; the last two drive the manifest
// registration flow from the HTTP handlers.
type Service interface {
	Current(ctx context.Context) (Credentials, error)
	AppCredentials(ctx context.Context) (appID, privateKeyPEM string, ok bool)
	WebhookSecret(ctx context.Context) string
	AppConfig(ctx context.Context) (appID, slug string, configured bool)
	InstallURL(ctx context.Context, state string) (string, bool)
	BeginRegistration(ctx context.Context, in BeginRegistrationInput) (BeginRegistrationResult, error)
	CompleteRegistration(ctx context.Context, in CompleteRegistrationInput) (CompleteRegistrationResult, error)
}

// Deps are what the githubapp module needs. Crypto, GitHub, Policy, and Audit are CONSUMER-DEFINED
// ports (satisfied by *crypto.Box, *github.Client, *policy.Service, *audit.Service), wired in
// internal/app — githubapp imports none of those modules. Env is the operator's GITHUB_APP_* config
// (takes precedence over stored). BaseURL is the dashboard origin; WebhookURL is the control plane's
// public webhook endpoint.
type Deps struct {
	DB         *database.DB
	Crypto     Sealer
	GitHub     ManifestConverter
	Policy     authz.Authorizer
	Audit      Recorder
	Env        EnvConfig
	BaseURL    string
	WebhookURL string
	Log        *slog.Logger
}

// Module is the githubapp module: the only wiring surface other code touches.
type Module struct {
	service Service
}

// New assembles the service over its ports.
func New(d Deps) *Module {
	store := newPostgresStore(d.DB)
	return &Module{
		service: newService(d.DB, store, d.Crypto, d.GitHub, d.Policy, d.Audit, d.Env, d.BaseURL, d.WebhookURL, d.Log),
	}
}

// Service exposes the module's service (for internal/app HTTP handlers + the resolver wiring).
func (m *Module) Service() Service { return m.service }
