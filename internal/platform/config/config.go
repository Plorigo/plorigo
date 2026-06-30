// Package config loads typed configuration from the environment (12-factor).
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config is the process configuration shared by all three binaries.
type Config struct {
	MasterKey   string
	DatabaseURL string
	Port        string
	Dev         bool

	// BaseURL is the dashboard origin used to build links in emails. In dev the
	// dashboard runs on the Vite server; in the single binary it is the control
	// plane's own public URL.
	BaseURL string
	// PublicURL is the control plane's own public URL — where the SERVER AGENT and the
	// public API are reached. It equals the origin users hit in the single-binary
	// deployment, but differs from BaseURL in a split/dev setup (dashboard vs API), so
	// the agent install command uses this, not BaseURL.
	PublicURL string
	// AllowOpenRegistration lets anyone register; when false only the first
	// (bootstrap) user and invited users may.
	AllowOpenRegistration bool
	// RequireEmailVerification sends a verification email on registration.
	RequireEmailVerification bool

	// SMTP is optional; when SMTPHost is empty, emails are logged instead of sent.
	SMTPHost  string
	SMTPPort  string
	SMTPUser  string
	SMTPPass  string
	EmailFrom string

	// GitHub OAuth App credentials for importing repositories. Optional: when unset,
	// the "Connect GitHub" flow is reported as not configured and the dashboard
	// disables it. The OAuth callback is served at BaseURL + "/api/github/callback"
	// (the dashboard origin, where the browser and the state cookie live), which must
	// match the OAuth App's registered authorization callback URL.
	GitHubClientID     string
	GitHubClientSecret string
	GitHubScopes       string

	// GitHub App credentials (optional), for reading PRIVATE repos/PRs with short-lived
	// per-installation tokens and verifying inbound webhook signatures. The private key and
	// webhook secret are control-plane-only: never returned by an RPC, never logged, never sent to
	// the agent (see docs/architecture/security.md). GitHubAppSlug is the App's URL slug, used to
	// build its installation URL. When unset, App features are reported as not configured.
	GitHubAppID         string
	GitHubAppPrivateKey string
	GitHubAppSlug       string
	GitHubWebhookSecret string
}

// Load reads configuration from the environment, applying defaults.
func Load() Config {
	// Secure by default: the app is in dev mode ONLY when PLORIGO_ENV explicitly names a
	// dev environment. Unset / typo / "production" all mean production, so a deploy that
	// forgets the var still gets Secure cookies + the CSRF guard, never the reverse.
	env := strings.ToLower(strings.TrimSpace(os.Getenv("PLORIGO_ENV")))
	baseURL := envOr("PLORIGO_BASE_URL", "http://localhost:5173")
	port := envOr("PORT", "8080")
	publicURLDefault := baseURL
	if env == "dev" || env == "development" || env == "local" {
		publicURLDefault = "http://localhost:" + port
	}
	return Config{
		MasterKey:   os.Getenv("APP_MASTER_KEY"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        port,
		Dev:         env == "dev" || env == "development" || env == "local",

		BaseURL: baseURL,
		// Defaults to BaseURL in production where the dashboard and API share one
		// origin; in dev, dashboard and API are split, so default to the API port.
		PublicURL:                envOr("PLORIGO_PUBLIC_URL", publicURLDefault),
		AllowOpenRegistration:    envBool("PLORIGO_ALLOW_OPEN_REGISTRATION", true),
		RequireEmailVerification: envBool("PLORIGO_REQUIRE_EMAIL_VERIFICATION", false),

		SMTPHost:  os.Getenv("SMTP_HOST"),
		SMTPPort:  envOr("SMTP_PORT", "587"),
		SMTPUser:  os.Getenv("SMTP_USERNAME"),
		SMTPPass:  os.Getenv("SMTP_PASSWORD"),
		EmailFrom: envOr("EMAIL_FROM", "no-reply@plorigo.local"),

		GitHubClientID:     os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		// "repo" grants access to private repositories too; narrow to "public_repo" by
		// overriding GITHUB_OAUTH_SCOPES if only public repos should be importable.
		GitHubScopes: envOr("GITHUB_OAUTH_SCOPES", "repo"),

		GitHubAppID:         os.Getenv("GITHUB_APP_ID"),
		GitHubAppPrivateKey: os.Getenv("GITHUB_APP_PRIVATE_KEY"),
		GitHubAppSlug:       os.Getenv("GITHUB_APP_SLUG"),
		GitHubWebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),
	}
}

// GitHubAppInstallURL builds the URL that starts a GitHub App installation, carrying state so the
// callback can tie the new installation to the requesting workspace. Empty when no App slug is
// configured (the dashboard then hides the App-connect option).
func (c Config) GitHubAppInstallURL(state string) string {
	if c.GitHubAppSlug == "" {
		return ""
	}
	return "https://github.com/apps/" + c.GitHubAppSlug + "/installations/new?state=" + url.QueryEscape(state)
}

// GitHubRedirectURL is the OAuth callback URL; it must match the OAuth App's registered
// authorization callback URL. It is built from BaseURL — the OAuth handshake is a
// browser flow, so the callback (and its state cookie) belong on the dashboard origin,
// which is proxied to the control plane in dev and shares the origin in production.
func (c Config) GitHubRedirectURL() string {
	return strings.TrimRight(c.BaseURL, "/") + "/api/github/callback"
}

// Validate checks the requirements for running the control plane.
func (c Config) Validate() error {
	var missing []string
	if c.MasterKey == "" {
		missing = append(missing, "APP_MASTER_KEY")
	}
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(key))) {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
