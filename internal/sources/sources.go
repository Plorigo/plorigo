// Package sources owns a workspace's connection to a Git provider (GitHub via OAuth in
// this slice) and each project's connected repository + branch. It is a PRIVILEGED,
// project-scoped module: every mutation is authorized via the neutral authz.Authorizer
// port (satisfied by the policy module) before it runs, and audited in the same
// transaction. A project source is project-scoped, so the owning workspace is resolved
// through the parent project (see store.go).
//
// The OAuth access token is ENCRYPTED at rest (AES-256-GCM via the SecretBox port,
// keyed by APP_MASTER_KEY). Unlike a secret it is OPENED server-side to call the
// provider on the user's behalf, but it remains WRITE-ONLY on the API: no RPC returns
// it and it is never logged. Building/cloning from a connected source is a later slice;
// this module records and displays the connection only. See
// docs/architecture/security.md and modules.md.
package sources

import (
	"context"
	"net/url"
	"time"
)

// provider is the OAuth Git provider; providerApp is a GitHub App installation. Both are stored on
// source_connections rows (one of each per workspace) so the schema and queries stay
// provider-agnostic. See 00030_github_app.sql.
const (
	provider    = "github"
	providerApp = "github_app"
)

// Connection is a workspace's link to a Git provider account (one per workspace). It
// carries metadata only — the sealed OAuth token never appears here.
type Connection struct {
	ID           string
	WorkspaceID  string
	Provider     string
	GitHubLogin  string
	GitHubUserID *int64
	Scopes       string
	ConnectedBy  *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ConnectionStatus is what GetConnection returns: whether the server has OAuth configured at all,
// whether this workspace is connected (Connection set if so), and the same for the GitHub App
// (configured server-side + an installation connected for this workspace).
type ConnectionStatus struct {
	Configured    bool
	Connected     bool
	Connection    Connection
	AppConfigured bool
	AppConnected  bool
}

// Repository is a candidate repo the connected account can access, offered in the
// dashboard's picker. It is never persisted.
type Repository struct {
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
	IsPrivate     bool
	HTMLURL       string
	Description   string
}

// ListReposInput lists repositories the workspace's connection can access. Query is an
// optional case-insensitive filter on full name; Page is 1-based (0 = first page).
type ListReposInput struct {
	WorkspaceID string
	Query       string
	Page        int
}

// BeginAuthInput starts the OAuth flow for a workspace.
type BeginAuthInput struct {
	WorkspaceID string
}

// BeginAuthResult is what the OAuth begin step returns: the provider URL to redirect
// the browser to, and the sealed state to set as a cookie and verify on callback.
type BeginAuthResult struct {
	AuthorizeURL string
	State        string
}

// CompleteAuthInput carries the OAuth callback parameters and the sealed state cookie.
type CompleteAuthInput struct {
	Code        string
	State       string // the state query param echoed back by the provider
	CookieState string // the sealed state set by the begin step
}

// CompleteAuthResult is what the OAuth callback step returns, for the redirect back to
// the dashboard.
type CompleteAuthResult struct {
	WorkspaceID string
	GitHubLogin string
}

// OAuthConfig is the server's GitHub OAuth App configuration, injected from config.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	Scopes       string
	RedirectURL  string
}

// Configured reports whether an OAuth App is configured (gates the connect flow).
func (c OAuthConfig) Configured() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

// AppConfig is the server's GitHub App configuration, injected from config. Slug builds the
// installation URL; the App id + private key live on the GitHubClient (for minting tokens).
type AppConfig struct {
	AppID string
	Slug  string
}

// Configured reports whether a GitHub App is configured (gates the install flow).
func (c AppConfig) Configured() bool {
	return c.AppID != "" && c.Slug != ""
}

// InstallURL is the URL that starts a GitHub App installation, carrying state so the setup callback
// can tie the installation to the requesting workspace.
func (c AppConfig) InstallURL(state string) string {
	return "https://github.com/apps/" + c.Slug + "/installations/new?state=" + url.QueryEscape(state)
}

// CompleteAppInput carries the GitHub App setup-callback parameters and the sealed state cookie.
type CompleteAppInput struct {
	InstallationID string // the installation_id GitHub appends to the setup URL
	SetupAction    string // "install" | "update" | "request"
	State          string // the nonce echoed back by GitHub
	CookieState    string // the sealed state set by the begin step
}

// Service is the surface other code (handlers, the OAuth HTTP handlers, internal/app,
// tests) depends on. BeginGitHubAuth/CompleteGitHubAuth drive the browser OAuth flow
// (called by the plain HTTP handlers); the rest back the SourceService RPCs — workspace
// connection + repository/branch DISCOVERY. Connecting a repository to a service lives in
// the services module (the repo is folded onto the service). No method ever returns the
// access token.
type Service interface {
	BeginGitHubAuth(ctx context.Context, in BeginAuthInput) (BeginAuthResult, error)
	CompleteGitHubAuth(ctx context.Context, in CompleteAuthInput) (CompleteAuthResult, error)

	// GitHub App installation flow (mirrors the OAuth flow, but connects an installation rather
	// than minting a user token). BeginAppInstall returns the install URL + sealed state;
	// CompleteAppInstall stores the installation for the workspace on the setup callback.
	BeginAppInstall(ctx context.Context, in BeginAuthInput) (BeginAuthResult, error)
	CompleteAppInstall(ctx context.Context, in CompleteAppInput) (CompleteAuthResult, error)

	GetConnection(ctx context.Context, workspaceID string) (ConnectionStatus, error)
	DisconnectGitHub(ctx context.Context, workspaceID string) error
	ListRepositories(ctx context.Context, in ListReposInput) ([]Repository, error)
	ListBranches(ctx context.Context, workspaceID, owner, repo string) ([]string, error)

	// InstallationToken mints a short-lived GitHub App installation access token for a workspace's
	// connected installation, for server-side PRIVATE-repo reads (the deploy/preview path). It is
	// NEVER returned by an RPC, logged, or sent to the agent. ok is false when no App installation
	// is connected. This is an internal seam (credential-resolution), not policy-authorized.
	InstallationToken(ctx context.Context, workspaceID string) (token string, ok bool, err error)
}
