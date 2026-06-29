// Package github is a minimal GitHub REST + OAuth client built on net/http. It is the
// provider adapter behind the sources module: the sources service reaches it through a
// consumer-defined port to exchange an OAuth code for a token, identify the connected
// account, and read repositories and branches.
//
// It holds no Plorigo types. Transport and HTTP-status failures map to a small set of
// typed sentinel errors (ErrNotFound, ErrUnauthorized, ErrForbidden, ErrRateLimited)
// that the caller translates into domain errors — keeping this package free of the app
// error vocabulary. See docs/architecture/security.md.
package github

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL   = "https://api.github.com"
	defaultOAuthBaseURL = "https://github.com"
	defaultTimeout      = 10 * time.Second
	apiVersion          = "2022-11-28"
	acceptJSON          = "application/vnd.github+json"
	acceptRaw           = "application/vnd.github.raw"
	// maxFileBytes caps a single file read (framework detection only needs small text files
	// like package.json or a lockfile); it bounds memory against a hostile or huge file.
	maxFileBytes = 1 << 20
)

// Sentinel errors. The caller (sources service) maps these to plain-English domain
// errors; the cause is intentionally coarse so a message never reveals more than the
// status already did (e.g. a private repo and a missing repo both surface as NotFound).
var (
	ErrNotFound     = errors.New("github: not found")
	ErrUnauthorized = errors.New("github: unauthorized")
	ErrForbidden    = errors.New("github: forbidden")
	ErrRateLimited  = errors.New("github: rate limited")
)

// Config configures a Client. The base URLs default to the public GitHub endpoints and
// are overridable so tests can point at an httptest server.
type Config struct {
	APIBaseURL   string // default https://api.github.com
	OAuthBaseURL string // default https://github.com
	HTTPClient   *http.Client
}

// Client talks to GitHub. It is safe for concurrent use.
type Client struct {
	apiBaseURL   string
	oauthBaseURL string
	http         *http.Client
}

// NewClient builds a Client, filling defaults for any zero Config field.
func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		apiBaseURL:   strings.TrimRight(cmp.Or(cfg.APIBaseURL, defaultAPIBaseURL), "/"),
		oauthBaseURL: strings.TrimRight(cmp.Or(cfg.OAuthBaseURL, defaultOAuthBaseURL), "/"),
		http:         httpClient,
	}
}

// Token is an OAuth access token plus the scopes it was granted.
type Token struct {
	AccessToken string
	Scope       string
}

// User is the authenticated account behind a token.
type User struct {
	Login string
	ID    int64
}

// RepoInfo is the subset of repository metadata Plorigo stores or displays.
type RepoInfo struct {
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
	Private       bool
	HTMLURL       string
	Description   string
}

// PullRequest is the subset of a GitHub pull request Plorigo needs to build and link a
// preview deployment: its head ref (the branch to build), head commit SHA, web URL, title,
// and state.
type PullRequest struct {
	Number  int
	State   string // "open" | "closed"
	Title   string
	HTMLURL string
	HeadRef string // the head branch name — what the preview builds
	HeadSHA string
}

// ListReposOptions tunes ListUserRepos. Zero values fall back to sensible defaults.
type ListReposOptions struct {
	Page    int    // 1-based; <=0 means page 1
	PerPage int    // <=0 means 100
	Sort    string // e.g. "updated"; "" means GitHub's default
}

// AuthorizeURL builds the URL the browser is redirected to so the user can approve the
// OAuth App. state is echoed back to the callback for CSRF verification.
func (c *Client) AuthorizeURL(clientID, redirectURI, scopes, state string) string {
	v := url.Values{}
	v.Set("client_id", clientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("scope", scopes)
	v.Set("state", state)
	v.Set("allow_signup", "false")
	return c.oauthBaseURL + "/login/oauth/authorize?" + v.Encode()
}

// ExchangeCode trades an authorization code for an access token. GitHub returns HTTP 200
// even on failure, carrying an "error" field — so a missing token or a present error is
// treated as a failure regardless of status.
func (c *Client) ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (Token, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.oauthBaseURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("github: build token request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("github: exchange code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return Token{}, classify(resp)
	}
	var body struct {
		AccessToken      string `json:"access_token"`
		Scope            string `json:"scope"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Token{}, fmt.Errorf("github: decode token response: %w", err)
	}
	if body.Error != "" || body.AccessToken == "" {
		desc := cmp.Or(body.ErrorDescription, body.Error, "no access token returned")
		return Token{}, fmt.Errorf("%w: %s", ErrUnauthorized, desc)
	}
	return Token{AccessToken: body.AccessToken, Scope: body.Scope}, nil
}

// GetAuthenticatedUser returns the account the token belongs to.
func (c *Client) GetAuthenticatedUser(ctx context.Context, token string) (User, error) {
	var body struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	}
	if err := c.getJSON(ctx, token, "/user", &body); err != nil {
		return User{}, err
	}
	return User{Login: body.Login, ID: body.ID}, nil
}

// GetRepository returns metadata for a single repository, or ErrNotFound if it does not
// exist or the token cannot see it.
func (c *Client) GetRepository(ctx context.Context, token, owner, repo string) (RepoInfo, error) {
	var body repoJSON
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	if err := c.getJSON(ctx, token, path, &body); err != nil {
		return RepoInfo{}, err
	}
	return body.toRepoInfo(), nil
}

// ListUserRepos lists repositories the token's account can access.
func (c *Client) ListUserRepos(ctx context.Context, token string, opts ListReposOptions) ([]RepoInfo, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(cmp.Or(opts.PerPage, 100)))
	q.Set("page", strconv.Itoa(cmp.Or(opts.Page, 1)))
	if opts.Sort != "" {
		q.Set("sort", opts.Sort)
	}
	var body []repoJSON
	if err := c.getJSON(ctx, token, "/user/repos?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	repos := make([]RepoInfo, 0, len(body))
	for _, r := range body {
		repos = append(repos, r.toRepoInfo())
	}
	return repos, nil
}

// ListBranches returns the branch names of a repository (first page, up to 100).
func (c *Client) ListBranches(ctx context.Context, token, owner, repo string) ([]string, error) {
	var body []struct {
		Name string `json:"name"`
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/branches?per_page=100"
	if err := c.getJSON(ctx, token, path, &body); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(body))
	for _, b := range body {
		names = append(names, b.Name)
	}
	return names, nil
}

// GetBranch reports whether a branch exists: nil if it does, ErrNotFound if it does not.
// It validates a chosen branch without paging the full branch list (which is capped).
func (c *Client) GetBranch(ctx context.Context, token, owner, repo, branch string) error {
	var ignore struct {
		Name string `json:"name"`
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/branches/" + url.PathEscape(branch)
	return c.getJSON(ctx, token, path, &ignore)
}

// GetPullRequest returns a single pull request by number, or ErrNotFound if it does not exist
// or the token cannot see it. token may be empty for a public repository.
func (c *Client) GetPullRequest(ctx context.Context, token, owner, repo string, number int) (PullRequest, error) {
	var body pullRequestJSON
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/pulls/" + strconv.Itoa(number)
	if err := c.getJSON(ctx, token, path, &body); err != nil {
		return PullRequest{}, err
	}
	return body.toPullRequest(), nil
}

// GetFileContent returns the raw bytes of a single file at ref (a branch, tag, or commit SHA).
// ok is false (nil error) when the file does not exist. token may be empty for a public repo.
// It is used by framework detection to read a repo's package.json, lockfile, and configs.
func (c *Client) GetFileContent(ctx context.Context, token, owner, repo, ref, path string) ([]byte, bool, error) {
	u := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/contents/" + escapeContentsPath(path)
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	data, err := c.getRaw(ctx, token, u)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// escapeContentsPath escapes each segment of a repo file path while keeping the separators, so
// a nested path is a valid contents-API path.
func escapeContentsPath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// RevokeToken revokes a single OAuth access token for this app at GitHub (the app
// authorization is otherwise left intact). It authenticates with the app's client
// credentials, not the token. Best-effort by contract: callers log and continue on
// failure, since the local record is the source of truth.
func (c *Client) RevokeToken(ctx context.Context, clientID, clientSecret, token string) error {
	body, err := json.Marshal(map[string]string{"access_token": token})
	if err != nil {
		return fmt.Errorf("github: marshal revoke body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.apiBaseURL+"/applications/"+url.PathEscape(clientID)+"/token", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("github: build revoke request: %w", err)
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Accept", acceptJSON)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github: revoke token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// 204 = revoked. 404/422 mean GitHub no longer knows this token (already invalid),
	// which is the desired end state, so treat them as success too.
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusUnprocessableEntity {
		return classify(resp)
	}
	return nil
}

// repoJSON is the wire shape of a GitHub repository; mapped to RepoInfo.
type repoJSON struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
	Description   string `json:"description"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

func (r repoJSON) toRepoInfo() RepoInfo {
	return RepoInfo{
		Owner:         r.Owner.Login,
		Name:          r.Name,
		FullName:      r.FullName,
		DefaultBranch: r.DefaultBranch,
		Private:       r.Private,
		HTMLURL:       r.HTMLURL,
		Description:   r.Description,
	}
}

// pullRequestJSON is the wire shape of a GitHub pull request; mapped to PullRequest.
type pullRequestJSON struct {
	Number  int    `json:"number"`
	State   string `json:"state"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	Head    struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

func (p pullRequestJSON) toPullRequest() PullRequest {
	return PullRequest{
		Number:  p.Number,
		State:   p.State,
		Title:   p.Title,
		HTMLURL: p.HTMLURL,
		HeadRef: p.Head.Ref,
		HeadSHA: p.Head.SHA,
	}
}

// getJSON performs an authenticated GET against the API base URL and decodes a 2xx body
// into out, mapping non-2xx responses to sentinel errors.
func (c *Client) getJSON(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", acceptJSON)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return classify(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("github: decode %s: %w", path, err)
	}
	return nil
}

// getRaw performs an authenticated GET and returns the (capped) response body, mapping non-2xx
// responses to sentinel errors. Unlike getJSON it asks for the raw media type, so the GitHub
// contents API returns the file bytes directly instead of a base64-wrapped JSON envelope.
func (c *Client) getRaw(ctx context.Context, token, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", acceptRaw)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		return nil, classify(resp)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes))
	if err != nil {
		return nil, fmt.Errorf("github: read %s: %w", path, err)
	}
	return data, nil
}

// classify maps a non-2xx response to a sentinel error. A 403 with the rate-limit
// budget exhausted (or a 429) is rate limiting; any other 403 is a permission denial.
func classify(resp *http.Response) error {
	// Drain a little of the body so the connection can be reused; ignore errors.
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	switch resp.StatusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return ErrRateLimited
		}
		return ErrForbidden
	case http.StatusTooManyRequests:
		return ErrRateLimited
	default:
		return fmt.Errorf("github: unexpected status %d", resp.StatusCode)
	}
}
