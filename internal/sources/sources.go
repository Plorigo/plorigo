// Package sources owns a workspace's integrations — its connections to Git providers (GitHub today;
// GitLab/etc. via the providers seam later). A workspace may have MANY connections: several OAuth
// accounts and/or App installations, across providers. Each connection is one row; a service
// references the specific connection it builds from (services.connection_id). All VCS access goes
// through the providers.Registry, so this module is the single provider-aware seam — services,
// deployments, and webhooks reach providers only through it.
//
// Credentials are control-plane-only: an OAuth token is sealed at rest (AES-256-GCM via SecretBox)
// and opened only to call the provider; an App connection stores only an installation_id and mints
// short-lived tokens on demand. Neither is ever returned by an RPC or logged. See
// docs/architecture/security.md and sources.md.
package sources

import (
	"context"
	"time"
)

// Connection kinds. A connection is reached either with an OAuth token or via an App installation.
const (
	kindOAuth = "oauth"
	kindApp   = "app"
)

// Connection is one integration: a workspace's link to a provider account (oauth) or App
// installation (app). Metadata only — the sealed token never appears here.
type Connection struct {
	ID             string
	WorkspaceID    string
	Provider       string // "github" (providers.Registry id)
	Kind           string // "oauth" | "app"
	AccountLogin   string // the connected account/org login
	AccountID      *int64
	InstallationID *string // app connections only
	Scopes         string  // oauth connections only
	ConnectedBy    *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ProviderStatus reports, per provider, what the SERVER has configured so the dashboard can offer
// the right connect actions (and show "coming soon" for providers that exist but aren't configured).
type ProviderStatus struct {
	Provider        string
	DisplayName     string
	OAuthConfigured bool
	AppConfigured   bool
	Available       bool // implemented at all (true for GitHub)
}

// ListConnectionsResult is what ListConnections returns: the workspace's connections plus the
// per-provider server-config status.
type ListConnectionsResult struct {
	Connections []Connection
	Providers   []ProviderStatus
}

// Repository is a candidate repo a connection can access, offered in the dashboard's picker.
type Repository struct {
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
	IsPrivate     bool
	HTMLURL       string
	Description   string
}

// ResolvedRepo is the validated repo facts the services module needs to record a git service from a
// chosen connection (returned by ValidateRepo). Buildable comes from the provider (App installs are
// buildable; OAuth is discovery-only for GitHub).
type ResolvedRepo struct {
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
	HTMLURL       string
	IsPrivate     bool
	Branch        string // resolved (validated, or defaulted to DefaultBranch)
	Provider      string
	Kind          string
	AccountLogin  string
	Buildable     bool
}

// ListReposInput lists repositories a connection can access. Query filters full name
// (case-insensitive); Page is 1-based (0 = first page).
type ListReposInput struct {
	ConnectionID string
	Query        string
	Page         int
}

// BeginConnectInput starts a connect flow (OAuth or App install) for a workspace + provider.
type BeginConnectInput struct {
	WorkspaceID string
	Provider    string
}

// BeginAuthResult is what a begin step returns: the URL to redirect the browser to and the sealed
// state to set as a cookie and verify on callback.
type BeginAuthResult struct {
	AuthorizeURL string
	State        string
}

// CompleteOAuthInput carries the OAuth callback params + sealed state cookie. Provider comes from the
// route the callback was served on.
type CompleteOAuthInput struct {
	Provider    string
	Code        string
	State       string
	CookieState string
}

// CompleteAppInput carries the App setup-callback params + sealed state cookie.
type CompleteAppInput struct {
	Provider       string
	InstallationID string
	SetupAction    string
	State          string
	CookieState    string
}

// CompleteAuthResult is what a complete step returns, for the redirect back to the dashboard.
type CompleteAuthResult struct {
	WorkspaceID  string
	AccountLogin string
}

// Service is the surface other code depends on. Begin/Complete drive the browser connect flows
// (called by the per-provider HTTP handlers); ListConnections/ListRepositories/ListBranches/
// DisconnectConnection back the SourceService RPCs. InstallationToken/GetConnectionMeta/ValidateRepo
// are internal seams (not policy-authorized) for the deployments + services modules. No method ever
// returns a token.
type Service interface {
	BeginOAuth(ctx context.Context, in BeginConnectInput) (BeginAuthResult, error)
	CompleteOAuth(ctx context.Context, in CompleteOAuthInput) (CompleteAuthResult, error)
	BeginAppInstall(ctx context.Context, in BeginConnectInput) (BeginAuthResult, error)
	CompleteAppInstall(ctx context.Context, in CompleteAppInput) (CompleteAuthResult, error)

	ListConnections(ctx context.Context, workspaceID string) (ListConnectionsResult, error)
	DisconnectConnection(ctx context.Context, connectionID string) error
	ListRepositories(ctx context.Context, in ListReposInput) ([]Repository, error)
	ListBranches(ctx context.Context, connectionID, owner, repo string) ([]string, error)

	// InstallationToken mints a short-lived App installation token for the given connection, for
	// server-side PRIVATE-repo reads (the deploy/preview path). ok is false when the connection is
	// not an App connection. Internal seam — never returned by an RPC, logged, or (here) sent to the
	// agent; the deployments service forwards it to the agent in the signed job.
	InstallationToken(ctx context.Context, connectionID string) (token string, ok bool, err error)
	// GetConnectionMeta returns a connection's metadata by id (provider, kind, workspace, account).
	// Internal seam for the services module. ok is false when the connection does not exist.
	GetConnectionMeta(ctx context.Context, connectionID string) (Connection, bool, error)
	// ValidateRepo validates owner/repo (+ optional branch) against a connection's provider and
	// returns the repo facts the services module records. Internal seam (the caller authorizes the
	// service create).
	ValidateRepo(ctx context.Context, connectionID, owner, repo, branch string) (ResolvedRepo, error)
}
