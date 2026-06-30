// Package githubapp owns the instance's server-wide GitHub App credentials and the automated
// registration flow (GitHub's App-manifest flow). A single App serves every workspace: it mints the
// short-lived per-installation tokens that read private repos and signs nothing the agent sees.
//
// Credentials come from one of two places, env first: the GITHUB_APP_* environment variables
// (operator-set, takes precedence), or — when those are unset — a sealed singleton row written by
// the manifest flow. Either way the private key and webhook secret are control-plane-only: never
// returned by an RPC, never logged, never sent to the agent. See docs/architecture/sources.md and
// security.md. This module exposes resolver methods (AppCredentials, WebhookSecret, AppConfig,
// InstallURL) that the github client, the webhook handler, and the sources module consume.
package githubapp

import (
	"encoding/json"
	"strings"
)

// Credentials is the fully-resolved active GitHub App configuration. The PEM and secrets are opened
// in-process and must never be logged or returned by an RPC. Configured is false when no App is set
// up (neither env nor stored).
type Credentials struct {
	AppID         string
	Slug          string
	PrivateKeyPEM string
	WebhookSecret string
	ClientID      string
	ClientSecret  string
	Configured    bool
	// Source is "env" or "stored" (for diagnostics/logging of non-secret provenance); empty when
	// not configured.
	Source string
}

// BeginRegistrationInput starts the manifest flow for a workspace. Org, when set, registers the App
// under that GitHub organization instead of the calling user's account.
type BeginRegistrationInput struct {
	WorkspaceID string
	Org         string
}

// BeginRegistrationResult is what the HTTP handler needs to drive the browser into GitHub's
// "create App from manifest" page: the manifest JSON to POST, the form action URL (account or org),
// and the sealed state to set as a cookie and verify on the callback.
type BeginRegistrationResult struct {
	ManifestJSON string
	FormAction   string
	State        string
}

// CompleteRegistrationInput finishes the flow on the manifest callback: GitHub appends a temporary
// code (exchanged for the App's credentials) and echoes the nonce as state, verified against the
// sealed CookieState.
type CompleteRegistrationInput struct {
	Code        string
	State       string
	CookieState string
}

// CompleteRegistrationResult reports the newly-registered App's non-secret identity for the UI.
type CompleteRegistrationResult struct {
	Slug  string
	AppID string
}

// manifest is GitHub's App-manifest shape (the subset Plorigo sets). The permissions/events mirror
// what the install + webhook + private-clone paths need: read contents/metadata/PRs and pull_request
// webhooks. We deliberately omit "name" so GitHub prompts the operator to choose a (globally-unique)
// name on the create page rather than failing on a collision.
type manifest struct {
	URL            string            `json:"url"`
	HookAttributes map[string]string `json:"hook_attributes"`
	RedirectURL    string            `json:"redirect_url"`
	SetupURL       string            `json:"setup_url,omitempty"`
	SetupOnUpdate  bool              `json:"setup_on_update"`
	Public         bool              `json:"public"`
	DefaultEvents  []string          `json:"default_events"`
	DefaultPerms   map[string]string `json:"default_permissions"`
}

// buildManifest renders the manifest JSON for this instance. baseURL is the dashboard origin (where
// the redirect/setup callbacks live); webhookURL is the control-plane public webhook endpoint.
func buildManifest(baseURL, webhookURL string) (string, error) {
	base := strings.TrimRight(baseURL, "/")
	m := manifest{
		URL:            base,
		HookAttributes: map[string]string{"url": webhookURL},
		RedirectURL:    base + "/api/github/app/manifest/callback",
		SetupURL:       base + "/api/github/app/setup",
		SetupOnUpdate:  true,
		Public:         false,
		DefaultEvents:  []string{"pull_request"},
		DefaultPerms: map[string]string{
			"contents":      "read",
			"metadata":      "read",
			"pull_requests": "read",
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
