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
	"time"
)

// provider is the only Git provider supported in this slice. It is stored on every row
// so the schema and queries are provider-agnostic; adding another provider would extend
// the `provider` CHECK constraint (a migration) rather than reshaping the tables.
const provider = "github"

// access records how a project source is reached, mirrored on the `access` column and
// Source.Access. A public source carries no connection and no credential; an oauth
// source resolves through the workspace's OAuth Connection. ('app' — the GitHub App — is
// a later slice; the column's CHECK already permits it.)
const (
	accessOAuth  = "oauth"
	accessPublic = "public"
)

// Source is a project's connected repository + branch (the domain model, independent of
// DB and transport types). It carries no token. WorkspaceID is resolved through the
// parent project for authorization/audit and is not part of the wire contract.
type Source struct {
	ID            string
	ProjectID     string
	ConnectionID  string
	WorkspaceID   string
	Provider      string
	Owner         string
	Repo          string
	FullName      string
	Branch        string
	DefaultBranch string
	IsPrivate     bool
	HTMLURL       string
	GitHubLogin   string // the connected account this source resolves through; empty when public
	Access        string // how the source is reached: "oauth" | "public" | "app"
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

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

// ConnectionStatus is what GetConnection returns: whether the server has OAuth
// configured at all, and whether this workspace is connected (Connection set if so).
type ConnectionStatus struct {
	Configured bool
	Connected  bool
	Connection Connection
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

// ConnectRepoInput selects the repository + branch to connect to a project.
type ConnectRepoInput struct {
	ProjectID string
	Owner     string
	Repo      string
	Branch    string
}

// ConnectPublicRepoInput connects a public repository to a project with no provider
// connection. RepoURL is a public repo URL ("https://github.com/owner/repo", with or
// without ".git") or a bare "owner/repo"; Branch is optional (empty selects the
// repository's default branch).
type ConnectPublicRepoInput struct {
	ProjectID string
	RepoURL   string
	Branch    string
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

// Service is the surface other code (handlers, the OAuth HTTP handlers, internal/app,
// tests) depends on. BeginGitHubAuth/CompleteGitHubAuth drive the browser OAuth flow
// (called by the plain HTTP handlers); the rest back the SourceService RPCs. No method
// ever returns the access token.
type Service interface {
	BeginGitHubAuth(ctx context.Context, in BeginAuthInput) (BeginAuthResult, error)
	CompleteGitHubAuth(ctx context.Context, in CompleteAuthInput) (CompleteAuthResult, error)

	GetConnection(ctx context.Context, workspaceID string) (ConnectionStatus, error)
	DisconnectGitHub(ctx context.Context, workspaceID string) error
	ListRepositories(ctx context.Context, in ListReposInput) ([]Repository, error)
	ListBranches(ctx context.Context, workspaceID, owner, repo string) ([]string, error)

	ConnectRepository(ctx context.Context, in ConnectRepoInput) (Source, error)
	ConnectPublicRepository(ctx context.Context, in ConnectPublicRepoInput) (Source, error)
	GetProjectSource(ctx context.Context, projectID string) (Source, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Source, error)
	DisconnectRepository(ctx context.Context, projectID string) error
}
