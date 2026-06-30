// The VCS provider seam lives here: the neutral Provider interface each Git host implements (GitHub
// today; GitLab/Bitbucket later) plus a Registry mapping a connection's provider id to its
// implementation. The sources service is written against this interface, so the rest of the control
// plane is provider-agnostic — adding a provider means implementing Provider and registering it, not
// touching services/deployments. Adapters translate their client's transport/status failures into
// the neutral sentinels below so the service maps errors without importing a provider's client.

package sources

import (
	"context"
	"errors"
)

// Neutral error sentinels. Adapters map their client errors to these; sources maps these to
// problem.* domain errors. The cause is coarse on purpose (a private vs missing repo both surface as
// NotFound), matching the platform/github client's own contract.
var (
	ErrNotFound     = errors.New("provider: not found")
	ErrUnauthorized = errors.New("provider: unauthorized")
	ErrForbidden    = errors.New("provider: forbidden")
	ErrRateLimited  = errors.New("provider: rate limited")
)

// Account is the identity behind a connection: the connected provider account or org.
type Account struct {
	Login string
	ID    *int64
}

// Repo is a repository a connection can access — a picker candidate, never persisted.
type Repo struct {
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
	HTMLURL       string
	Description   string
	Private       bool
}

// PullRequest is the subset of a pull/merge request needed to build + link a preview.
type PullRequest struct {
	Number  int
	State   string // "open" | "closed"
	Title   string
	HTMLURL string
	HeadRef string
	HeadSHA string
}

// Conn carries the per-connection facts a provider needs to act on a connection's behalf: its kind
// and the already-resolved credential (an opened OAuth token, or a freshly-minted App installation
// token). The sources service resolves the credential before calling repo/PR methods, so adapters
// never touch the sealed store.
type Conn struct {
	Kind           string // "oauth" | "app"
	Token          string // resolved credential for repo/PR calls
	InstallationID string // app connections only (for context/logging)
}

// Provider is what each VCS implements. OAuth + App methods drive the connect flows; the repo/PR
// methods read on behalf of a resolved connection; VerifyWebhook checks an inbound signature.
type Provider interface {
	ID() string          // stable id stored on the connection row, e.g. "github"
	DisplayName() string // human label for the UI, e.g. "GitHub"

	// Server-config status (drives which connect methods the UI offers).
	OAuthConfigured() bool
	AppConfigured(ctx context.Context) bool

	// OAuth flow.
	AuthorizeURL(state string) string
	ExchangeCode(ctx context.Context, code string) (token, scopes string, account Account, err error)
	RevokeToken(ctx context.Context, token string) error

	// App-installation flow.
	InstallURL(ctx context.Context, state string) (string, bool)
	ResolveInstallation(ctx context.Context, installationID string) (Account, error)
	InstallationToken(ctx context.Context, installationID string) (string, error)

	// Reads on behalf of a resolved connection.
	ListRepos(ctx context.Context, c Conn, query string, page int) ([]Repo, error)
	ListBranches(ctx context.Context, c Conn, owner, repo string) ([]string, error)
	GetRepository(ctx context.Context, c Conn, owner, repo string) (Repo, error)
	// GetBranch reports whether a branch exists (nil), or ErrNotFound if not — for validating a
	// chosen branch without paging the full list.
	GetBranch(ctx context.Context, c Conn, owner, repo, branch string) error
	GetPullRequest(ctx context.Context, c Conn, owner, repo string, number int) (PullRequest, error)

	// VerifyWebhook reports whether signature is valid for body keyed by secret (the caller resolves
	// the secret). Fails closed on an empty secret.
	VerifyWebhook(secret string, body []byte, signature string) bool

	// Buildable reports whether a connection of this kind can be built/deployed by the agent. For
	// GitHub: app installations yes (short-lived token), oauth no (broad token never sent to agent).
	Buildable(kind string) bool
}

// Registry resolves a provider by id and lists the configured providers (for the UI).
type Registry struct {
	byID  map[string]Provider
	order []string
}

// NewRegistry builds a registry from the given providers, preserving order for listing.
func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byID: make(map[string]Provider, len(ps))}
	for _, p := range ps {
		if p == nil {
			continue
		}
		r.byID[p.ID()] = p
		r.order = append(r.order, p.ID())
	}
	return r
}

// Get returns the provider for id, or ok=false when unregistered.
func (r *Registry) Get(id string) (Provider, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// All returns the registered providers in registration order.
func (r *Registry) All() []Provider {
	out := make([]Provider, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id])
	}
	return out
}
