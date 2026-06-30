package sources

import (
	"context"
	"errors"
	"strings"

	"github.com/plorigo/plorigo/internal/platform/github"
)

// GitHubClient is the subset of *github.Client the adapter uses (the App-aware client built in
// internal/app, which resolves app credentials dynamically via githubapp).
type GitHubClient interface {
	AuthorizeURL(clientID, redirectURI, scopes, state string) string
	ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (github.Token, error)
	GetAuthenticatedUser(ctx context.Context, token string) (github.User, error)
	RevokeToken(ctx context.Context, clientID, clientSecret, token string) error
	GetInstallation(ctx context.Context, installationID string) (github.Installation, error)
	InstallationToken(ctx context.Context, installationID string) (string, error)
	ListUserRepos(ctx context.Context, token string, opts github.ListReposOptions) ([]github.RepoInfo, error)
	ListInstallationRepos(ctx context.Context, token string, opts github.ListReposOptions) ([]github.RepoInfo, error)
	ListBranches(ctx context.Context, token, owner, repo string) ([]string, error)
	GetRepository(ctx context.Context, token, owner, repo string) (github.RepoInfo, error)
	GetBranch(ctx context.Context, token, owner, repo, branch string) error
	GetPullRequest(ctx context.Context, token, owner, repo string, number int) (github.PullRequest, error)
}

// GitHubAppConfig resolves the server's GitHub App config at call time (satisfied by
// *githubapp.Service): whether an App is configured + the installation URL.
type GitHubAppConfig interface {
	AppConfig(ctx context.Context) (appID, slug string, configured bool)
	InstallURL(ctx context.Context, state string) (string, bool)
}

// OAuthConfig is the server's GitHub OAuth App configuration.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	Scopes       string
	RedirectURL  string
}

// GitHub is the GitHub implementation of Provider. It wraps the platform github client (OAuth + App +
// repo reads), the server OAuth config, and the App-config resolver.
type GitHub struct {
	client GitHubClient
	oauth  OAuthConfig
	app    GitHubAppConfig
}

var _ Provider = (*GitHub)(nil)

// NewGitHub builds the GitHub provider adapter.
func NewGitHub(client GitHubClient, oauth OAuthConfig, app GitHubAppConfig) *GitHub {
	return &GitHub{client: client, oauth: oauth, app: app}
}

// ID is the provider id stored on a connection row.
func (g *GitHub) ID() string { return "github" }

// DisplayName is the human label for the UI.
func (g *GitHub) DisplayName() string { return "GitHub" }

// OAuthConfigured reports whether the server has GitHub OAuth client credentials.
func (g *GitHub) OAuthConfigured() bool { return g.oauth.ClientID != "" && g.oauth.ClientSecret != "" }

// AppConfigured reports whether a GitHub App is configured (env or registered).
func (g *GitHub) AppConfigured(ctx context.Context) bool {
	_, _, configured := g.app.AppConfig(ctx)
	return configured
}

// AuthorizeURL builds the OAuth authorize URL carrying state.
func (g *GitHub) AuthorizeURL(state string) string {
	return g.client.AuthorizeURL(g.oauth.ClientID, g.oauth.RedirectURL, g.oauth.Scopes, state)
}

// ExchangeCode trades an OAuth code for a token + the authenticated account.
func (g *GitHub) ExchangeCode(ctx context.Context, code string) (string, string, Account, error) {
	tok, err := g.client.ExchangeCode(ctx, g.oauth.ClientID, g.oauth.ClientSecret, code, g.oauth.RedirectURL)
	if err != nil {
		return "", "", Account{}, mapGitHubErr(err)
	}
	user, err := g.client.GetAuthenticatedUser(ctx, tok.AccessToken)
	if err != nil {
		return "", "", Account{}, mapGitHubErr(err)
	}
	id := user.ID
	return tok.AccessToken, tok.Scope, Account{Login: user.Login, ID: &id}, nil
}

// RevokeToken best-effort revokes an OAuth token at GitHub (no-op when unconfigured or empty).
func (g *GitHub) RevokeToken(ctx context.Context, token string) error {
	if token == "" || !g.OAuthConfigured() {
		return nil
	}
	return g.client.RevokeToken(ctx, g.oauth.ClientID, g.oauth.ClientSecret, token)
}

// InstallURL is the App installation URL carrying state, or ok=false when no App is configured.
func (g *GitHub) InstallURL(ctx context.Context, state string) (string, bool) {
	return g.app.InstallURL(ctx, state)
}

// ResolveInstallation resolves a new installation's account.
func (g *GitHub) ResolveInstallation(ctx context.Context, installationID string) (Account, error) {
	inst, err := g.client.GetInstallation(ctx, installationID)
	if err != nil {
		return Account{}, mapGitHubErr(err)
	}
	id := inst.AccountID
	return Account{Login: inst.Account, ID: &id}, nil
}

// InstallationToken mints a short-lived installation access token.
func (g *GitHub) InstallationToken(ctx context.Context, installationID string) (string, error) {
	tok, err := g.client.InstallationToken(ctx, installationID)
	return tok, mapGitHubErr(err)
}

// ListRepos lists the connection's accessible repos (installation repos for an App, user repos for
// OAuth), filtered by query.
func (g *GitHub) ListRepos(ctx context.Context, c Conn, query string, page int) ([]Repo, error) {
	var (
		infos []github.RepoInfo
		err   error
	)
	if c.Kind == "app" {
		infos, err = g.client.ListInstallationRepos(ctx, c.Token, github.ListReposOptions{Page: page})
	} else {
		infos, err = g.client.ListUserRepos(ctx, c.Token, github.ListReposOptions{Page: page, Sort: "updated"})
	}
	if err != nil {
		return nil, mapGitHubErr(err)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Repo, 0, len(infos))
	for _, r := range infos {
		if q != "" && !strings.Contains(strings.ToLower(r.FullName), q) {
			continue
		}
		out = append(out, toRepo(r))
	}
	return out, nil
}

// ListBranches lists a repo's branches.
func (g *GitHub) ListBranches(ctx context.Context, c Conn, owner, repo string) ([]string, error) {
	branches, err := g.client.ListBranches(ctx, c.Token, owner, repo)
	return branches, mapGitHubErr(err)
}

// GetRepository reads a repo's metadata.
func (g *GitHub) GetRepository(ctx context.Context, c Conn, owner, repo string) (Repo, error) {
	info, err := g.client.GetRepository(ctx, c.Token, owner, repo)
	if err != nil {
		return Repo{}, mapGitHubErr(err)
	}
	return toRepo(info), nil
}

// GetBranch reports whether a branch exists (nil) or ErrNotFound.
func (g *GitHub) GetBranch(ctx context.Context, c Conn, owner, repo, branch string) error {
	return mapGitHubErr(g.client.GetBranch(ctx, c.Token, owner, repo, branch))
}

// GetPullRequest reads a pull request's build/link facts.
func (g *GitHub) GetPullRequest(ctx context.Context, c Conn, owner, repo string, number int) (PullRequest, error) {
	pr, err := g.client.GetPullRequest(ctx, c.Token, owner, repo, number)
	if err != nil {
		return PullRequest{}, mapGitHubErr(err)
	}
	return PullRequest{Number: pr.Number, State: pr.State, Title: pr.Title, HTMLURL: pr.HTMLURL, HeadRef: pr.HeadRef, HeadSHA: pr.HeadSHA}, nil
}

// VerifyWebhook verifies an inbound webhook's HMAC signature.
func (g *GitHub) VerifyWebhook(secret string, body []byte, signature string) bool {
	return github.VerifyWebhookSignature(secret, body, signature)
}

// Buildable reports whether a connection of this kind can be built: an App installation (short-lived
// per-install token sent to the agent) yes; OAuth (broad token never sent to the agent) no.
func (g *GitHub) Buildable(kind string) bool { return kind == "app" }

func toRepo(r github.RepoInfo) Repo {
	return Repo{
		Owner: r.Owner, Name: r.Name, FullName: r.FullName, DefaultBranch: r.DefaultBranch,
		HTMLURL: r.HTMLURL, Description: r.Description, Private: r.Private,
	}
}

// mapGitHubErr translates the github client's sentinels into the neutral provider sentinels so the
// sources service maps errors without importing platform/github at the error layer.
func mapGitHubErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, github.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, github.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, github.ErrForbidden):
		return ErrForbidden
	case errors.Is(err, github.ErrRateLimited):
		return ErrRateLimited
	default:
		return err
	}
}
