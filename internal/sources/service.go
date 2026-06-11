package sources

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// stateTTL bounds how long an in-flight OAuth handshake stays valid.
const stateTTL = 10 * time.Minute

// service is the business logic. It orchestrates ports only — no SQL, no transport, and
// no cryptography of its own (it seals/opens through the SecretBox port). Every mutation
// authorizes the caller before the WithinTx block and audits inside it (modules.md, Rule
// 4). Provider calls happen BEFORE the transaction (network I/O must not run inside a DB
// tx). The access token is sealed before storage and opened only to call the provider;
// it is NEVER returned by any method or logged.
type service struct {
	tx         TxRunner
	store      Store
	box        SecretBox
	gh         GitHubClient
	oauth      OAuthConfig
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, box SecretBox, gh GitHubClient, oauth OAuthConfig, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, box: box, gh: gh, oauth: oauth, authorizer: authorizer, audit: audit, log: log}
}

var _ Service = (*service)(nil)

// oauthState is the sealed payload carried across the OAuth redirect. It binds the
// handshake to the workspace and the user that started it, with a nonce echoed back as
// the `state` parameter and an expiry.
type oauthState struct {
	WorkspaceID string `json:"w"`
	UserID      string `json:"u"`
	Nonce       string `json:"n"`
	ExpiresAt   int64  `json:"e"`
}

func (s *service) BeginGitHubAuth(ctx context.Context, in BeginAuthInput) (BeginAuthResult, error) {
	if _, err := id.Parse(in.WorkspaceID); err != nil {
		return BeginAuthResult{}, problem.InvalidInput("a valid workspace_id is required")
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: in.WorkspaceID}); err != nil {
		return BeginAuthResult{}, err
	}
	if !s.oauth.Configured() {
		return BeginAuthResult{}, problem.InvalidInput("GitHub OAuth is not configured on this server")
	}

	nonce, err := newNonce()
	if err != nil {
		return BeginAuthResult{}, problem.Internalf(err, "begin github auth")
	}
	payload, err := json.Marshal(oauthState{
		WorkspaceID: in.WorkspaceID,
		UserID:      caller.UserID,
		Nonce:       nonce,
		ExpiresAt:   time.Now().Add(stateTTL).Unix(),
	})
	if err != nil {
		return BeginAuthResult{}, problem.Internalf(err, "begin github auth")
	}
	sealed, err := s.box.Seal(payload)
	if err != nil {
		return BeginAuthResult{}, problem.Internalf(err, "seal oauth state")
	}
	return BeginAuthResult{
		AuthorizeURL: s.gh.AuthorizeURL(s.oauth.ClientID, s.oauth.RedirectURL, s.oauth.Scopes, nonce),
		State:        base64.RawURLEncoding.EncodeToString(sealed),
	}, nil
}

func (s *service) CompleteGitHubAuth(ctx context.Context, in CompleteAuthInput) (CompleteAuthResult, error) {
	if strings.TrimSpace(in.Code) == "" {
		return CompleteAuthResult{}, problem.InvalidInput("missing authorization code")
	}
	st, err := s.openState(in.CookieState)
	if err != nil {
		return CompleteAuthResult{}, err
	}
	// Constant work isn't needed here; both checks are equivalent failures.
	if in.State == "" || st.Nonce != in.State {
		return CompleteAuthResult{}, problem.InvalidInput("OAuth state mismatch; please try connecting again")
	}
	if time.Now().Unix() > st.ExpiresAt {
		return CompleteAuthResult{}, problem.InvalidInput("the GitHub connection request expired; please try again")
	}

	caller := principal.FromContext(ctx)
	// The user that completes must be the one that started the handshake.
	if !caller.IsAuthenticated() || caller.UserID != st.UserID {
		return CompleteAuthResult{}, problem.PermissionDenied("this GitHub connection was started by a different session")
	}
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: st.WorkspaceID}); err != nil {
		return CompleteAuthResult{}, err
	}

	token, err := s.gh.ExchangeCode(ctx, s.oauth.ClientID, s.oauth.ClientSecret, in.Code, s.oauth.RedirectURL)
	if err != nil {
		return CompleteAuthResult{}, mapGitHubErr(err)
	}
	user, err := s.gh.GetAuthenticatedUser(ctx, token.AccessToken)
	if err != nil {
		return CompleteAuthResult{}, mapGitHubErr(err)
	}
	// If this workspace was already connected, capture the previous token so it can be
	// revoked after the new one is stored (the upsert would otherwise orphan it, and the
	// old token would stay valid at GitHub).
	oldToken, _ := s.openConnectionToken(ctx, st.WorkspaceID)
	sealed, err := s.box.Seal([]byte(token.AccessToken))
	if err != nil {
		return CompleteAuthResult{}, problem.Internalf(err, "seal github token")
	}

	userID := caller.UserID
	githubUserID := user.ID
	var conn Connection
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		conn, txErr = s.store.UpsertConnection(ctx, tx, ConnectionWrite{
			WorkspaceID:     st.WorkspaceID,
			Provider:        provider,
			GitHubLogin:     user.Login,
			GitHubUserID:    &githubUserID,
			TokenCiphertext: sealed,
			Scopes:          token.Scope,
			ConnectedBy:     &userID,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "source.github.connect", "source_connection", conn.ID, st.WorkspaceID, caller.UserID)
	})
	if err != nil {
		return CompleteAuthResult{}, mapErr(err, "connect github")
	}
	// Revoke the superseded token, unless GitHub returned the same one on re-auth.
	if oldToken != "" && oldToken != token.AccessToken {
		s.revokeBestEffort(ctx, oldToken)
	}
	// Log the account login and never the token — the connection is write-only.
	s.log.Info("github connected", "workspace_id", st.WorkspaceID, "github_login", user.Login, "actor", caller.UserID)
	return CompleteAuthResult{WorkspaceID: st.WorkspaceID, GitHubLogin: user.Login}, nil
}

func (s *service) GetConnection(ctx context.Context, workspaceID string) (ConnectionStatus, error) {
	if _, err := id.Parse(workspaceID); err != nil {
		return ConnectionStatus{}, problem.InvalidInput("a valid workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceRead, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return ConnectionStatus{}, err
	}
	conn, ok, err := s.store.GetConnection(ctx, workspaceID, provider)
	if err != nil {
		return ConnectionStatus{}, problem.Internalf(err, "get connection")
	}
	return ConnectionStatus{Configured: s.oauth.Configured(), Connected: ok, Connection: conn}, nil
}

func (s *service) DisconnectGitHub(ctx context.Context, workspaceID string) error {
	if _, err := id.Parse(workspaceID); err != nil {
		return problem.InvalidInput("a valid workspace_id is required")
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceDisconnect, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return err
	}
	conn, ok, err := s.store.GetConnection(ctx, workspaceID, provider)
	if err != nil {
		return problem.Internalf(err, "disconnect github")
	}
	if !ok {
		return problem.NotFound("no GitHub connection for this workspace")
	}
	// A connection still used by projects must not be removed (a recovery path): the
	// caller disconnects those repositories first.
	count, err := s.store.CountProjectSourcesByConnection(ctx, conn.ID)
	if err != nil {
		return problem.Internalf(err, "disconnect github")
	}
	if count > 0 {
		return problem.InvalidInput("disconnect the %d project(s) using this GitHub connection first", count)
	}
	// Capture the current token before deleting the row so we can revoke it at GitHub —
	// OAuth-App tokens don't expire, so "disconnect" must cut off access, not just forget.
	oldToken, _ := s.openConnectionToken(ctx, workspaceID)
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		deletedID, deleted, txErr := s.store.DeleteConnection(ctx, tx, workspaceID, provider)
		if txErr != nil {
			return txErr
		}
		if !deleted {
			return problem.NotFound("no GitHub connection for this workspace")
		}
		return s.audit.Record(ctx, tx, "source.github.disconnect", "source_connection", deletedID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "disconnect github")
	}
	s.revokeBestEffort(ctx, oldToken)
	s.log.Info("github disconnected", "workspace_id", workspaceID, "actor", caller.UserID)
	return nil
}

func (s *service) ListRepositories(ctx context.Context, in ListReposInput) ([]Repository, error) {
	if _, err := id.Parse(in.WorkspaceID); err != nil {
		return nil, problem.InvalidInput("a valid workspace_id is required")
	}
	// Listing uses the connection's token, so it is gated like a connect action.
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: in.WorkspaceID}); err != nil {
		return nil, err
	}
	token, err := s.openConnectionToken(ctx, in.WorkspaceID)
	if err != nil {
		return nil, err
	}
	repos, err := s.gh.ListUserRepos(ctx, token, github.ListReposOptions{Page: in.Page, Sort: "updated"})
	if err != nil {
		return nil, mapGitHubErr(err)
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	out := make([]Repository, 0, len(repos))
	for _, r := range repos {
		if q != "" && !strings.Contains(strings.ToLower(r.FullName), q) {
			continue
		}
		out = append(out, toRepository(r))
	}
	return out, nil
}

func (s *service) ListBranches(ctx context.Context, workspaceID, owner, repo string) ([]string, error) {
	if _, err := id.Parse(workspaceID); err != nil {
		return nil, problem.InvalidInput("a valid workspace_id is required")
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
		return nil, problem.InvalidInput("owner and repo are required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	token, err := s.openConnectionToken(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	branches, err := s.gh.ListBranches(ctx, token, owner, repo)
	if err != nil {
		return nil, mapGitHubErr(err)
	}
	return branches, nil
}

func (s *service) ConnectRepository(ctx context.Context, in ConnectRepoInput) (Source, error) {
	if _, err := id.Parse(in.ProjectID); err != nil {
		return Source{}, problem.InvalidInput("a valid project_id is required")
	}
	owner, repo, branch := strings.TrimSpace(in.Owner), strings.TrimSpace(in.Repo), strings.TrimSpace(in.Branch)
	if owner == "" || repo == "" {
		return Source{}, problem.InvalidInput("owner and repo are required")
	}
	if branch == "" {
		return Source{}, problem.InvalidInput("a branch is required")
	}

	workspaceID, ok, err := s.store.WorkspaceIDForProject(ctx, in.ProjectID)
	if err != nil {
		return Source{}, problem.Internalf(err, "connect repository")
	}
	if !ok {
		return Source{}, problem.NotFound("project %s not found", in.ProjectID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return Source{}, err
	}

	conn, ok, err := s.store.GetConnection(ctx, workspaceID, provider)
	if err != nil {
		return Source{}, problem.Internalf(err, "connect repository")
	}
	if !ok {
		return Source{}, problem.NotFound("connect GitHub for this workspace first")
	}
	token, err := s.openConnectionToken(ctx, workspaceID)
	if err != nil {
		return Source{}, err
	}

	// Validate access and capture authoritative metadata from GitHub (don't trust
	// client-supplied owner/repo casing or privacy).
	info, err := s.gh.GetRepository(ctx, token, owner, repo)
	if err != nil {
		return Source{}, mapGitHubErr(err)
	}
	// Verify the chosen branch directly — the repo is known to exist (GetRepository
	// above), so a NotFound here means the branch, not the repo. A direct lookup avoids
	// the capped branch-list page rejecting a valid branch beyond the first 100.
	if err := s.gh.GetBranch(ctx, token, info.Owner, info.Name, branch); err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return Source{}, problem.InvalidInput("branch %q was not found in %s", branch, info.FullName)
		}
		return Source{}, mapGitHubErr(err)
	}

	var saved Source
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		saved, txErr = s.store.UpsertProjectSource(ctx, tx, ProjectSourceWrite{
			ProjectID:     in.ProjectID,
			ConnectionID:  conn.ID,
			Provider:      provider,
			Owner:         info.Owner,
			Repo:          info.Name,
			FullName:      info.FullName,
			Branch:        branch,
			DefaultBranch: info.DefaultBranch,
			IsPrivate:     info.Private,
			HTMLURL:       info.HTMLURL,
			Access:        accessOAuth,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "source.connect", "project_source", saved.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Source{}, mapErr(err, "connect repository")
	}
	saved.WorkspaceID = workspaceID
	saved.GitHubLogin = conn.GitHubLogin
	s.log.Info("repository connected", "project_id", in.ProjectID, "repo", info.FullName, "branch", branch, "actor", caller.UserID)
	return saved, nil
}

// ConnectPublicRepository connects a public repository (by URL) to a project with no
// provider connection and no stored credential. The repo is read UNAUTHENTICATED, so a
// private or missing repository is invisible and surfaces as NotFound. Authorization and
// auditing mirror ConnectRepository; only the credential-less provider reads differ.
func (s *service) ConnectPublicRepository(ctx context.Context, in ConnectPublicRepoInput) (Source, error) {
	if _, err := id.Parse(in.ProjectID); err != nil {
		return Source{}, problem.InvalidInput("a valid project_id is required")
	}
	owner, repo, err := parsePublicRepo(in.RepoURL)
	if err != nil {
		return Source{}, err
	}
	branch := strings.TrimSpace(in.Branch)

	workspaceID, ok, err := s.store.WorkspaceIDForProject(ctx, in.ProjectID)
	if err != nil {
		return Source{}, problem.Internalf(err, "connect public repository")
	}
	if !ok {
		return Source{}, problem.NotFound("project %s not found", in.ProjectID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return Source{}, err
	}

	// Empty token = anonymous request. Capture authoritative metadata (don't trust the
	// client's owner/repo casing) and confirm the repo is reachable without credentials.
	info, err := s.gh.GetRepository(ctx, "", owner, repo)
	if err != nil {
		return Source{}, mapGitHubErr(err)
	}
	if info.Private {
		// Defensive: an anonymous read of a private repo returns NotFound above, but if a
		// provider ever reported one as visible-but-private, refuse it on the public path.
		return Source{}, problem.InvalidInput("%s is private; connect GitHub to deploy a private repository", info.FullName)
	}
	// An empty branch defaults to the repo's default (known to exist); otherwise verify
	// the chosen branch directly, avoiding the capped branch-list page.
	if branch == "" {
		branch = info.DefaultBranch
	} else if err := s.gh.GetBranch(ctx, "", info.Owner, info.Name, branch); err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return Source{}, problem.InvalidInput("branch %q was not found in %s", branch, info.FullName)
		}
		return Source{}, mapGitHubErr(err)
	}

	var saved Source
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		saved, txErr = s.store.UpsertProjectSource(ctx, tx, ProjectSourceWrite{
			ProjectID:     in.ProjectID,
			ConnectionID:  "", // public: no connection, stored as NULL
			Provider:      provider,
			Owner:         info.Owner,
			Repo:          info.Name,
			FullName:      info.FullName,
			Branch:        branch,
			DefaultBranch: info.DefaultBranch,
			IsPrivate:     false,
			HTMLURL:       info.HTMLURL,
			Access:        accessPublic,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "source.connect", "project_source", saved.ID, workspaceID, caller.UserID)
	})
	if err != nil {
		return Source{}, mapErr(err, "connect public repository")
	}
	saved.WorkspaceID = workspaceID
	s.log.Info("public repository connected", "project_id", in.ProjectID, "repo", info.FullName, "branch", branch, "actor", caller.UserID)
	return saved, nil
}

// parsePublicRepo extracts owner and repo from a public GitHub reference: a full URL
// ("https://github.com/owner/repo", optionally ".git" or with extra path), the SSH form
// ("git@github.com:owner/repo.git"), or a bare "owner/repo". It rejects anything that
// does not resolve to owner/repo on github.com.
func parsePublicRepo(raw string) (owner, repo string, err error) {
	s := strings.TrimSpace(raw)
	bad := func() (string, string, error) {
		return "", "", problem.InvalidInput("enter a public GitHub repository URL like https://github.com/owner/repo")
	}
	if s == "" {
		return bad()
	}
	if rest, ok := strings.CutPrefix(s, "git@github.com:"); ok {
		s = rest
	} else {
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "http://")
		s = strings.TrimPrefix(s, "www.")
		if rest, ok := strings.CutPrefix(s, "github.com/"); ok {
			s = rest
		} else if first, _, found := strings.Cut(s, "/"); found && strings.Contains(first, ".") {
			// A host segment is present but it is not github.com.
			return bad()
		}
	}
	parts := strings.Split(strings.TrimPrefix(s, "/"), "/")
	if len(parts) < 2 {
		return bad()
	}
	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return bad()
	}
	return owner, repo, nil
}

func (s *service) GetProjectSource(ctx context.Context, projectID string) (Source, error) {
	if _, err := id.Parse(projectID); err != nil {
		return Source{}, problem.InvalidInput("a valid project_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceIDForProject(ctx, projectID)
	if err != nil {
		return Source{}, problem.Internalf(err, "get project source")
	}
	if !ok {
		return Source{}, problem.NotFound("project %s not found", projectID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceRead, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return Source{}, err
	}
	src, ok, err := s.store.GetProjectSource(ctx, projectID)
	if err != nil {
		return Source{}, problem.Internalf(err, "get project source")
	}
	if !ok {
		return Source{}, problem.NotFound("no repository is connected to this project")
	}
	src.WorkspaceID = workspaceID
	return src, nil
}

func (s *service) ListByWorkspace(ctx context.Context, workspaceID string) ([]Source, error) {
	if _, err := id.Parse(workspaceID); err != nil {
		return nil, problem.InvalidInput("a valid workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceRead, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByWorkspace(ctx, workspaceID)
}

func (s *service) DisconnectRepository(ctx context.Context, projectID string) error {
	if _, err := id.Parse(projectID); err != nil {
		return problem.InvalidInput("a valid project_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceIDForProject(ctx, projectID)
	if err != nil {
		return problem.Internalf(err, "disconnect repository")
	}
	if !ok {
		return problem.NotFound("project %s not found", projectID)
	}
	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceDisconnect, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return err
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		deletedID, deleted, txErr := s.store.DeleteProjectSource(ctx, tx, projectID)
		if txErr != nil {
			return txErr
		}
		if !deleted {
			return problem.NotFound("no repository is connected to this project")
		}
		return s.audit.Record(ctx, tx, "source.disconnect", "project_source", deletedID, workspaceID, caller.UserID)
	})
	if err != nil {
		return mapErr(err, "disconnect repository")
	}
	s.log.Info("repository disconnected", "project_id", projectID, "actor", caller.UserID)
	return nil
}

// revokeBestEffort revokes a token at the provider, logging and swallowing failures —
// the local record is the source of truth, so a failed revoke must not fail the action
// that already removed or replaced the token. No-ops on an empty token or unconfigured
// OAuth.
func (s *service) revokeBestEffort(ctx context.Context, token string) {
	if token == "" || !s.oauth.Configured() {
		return
	}
	if err := s.gh.RevokeToken(ctx, s.oauth.ClientID, s.oauth.ClientSecret, token); err != nil {
		s.log.Warn("failed to revoke github token at the provider (the local connection is already updated)", "error", err)
	}
}

// openConnectionToken loads and opens the workspace's sealed token for a server-side
// provider call. The plaintext stays in memory for the call only and is never returned
// to a caller.
func (s *service) openConnectionToken(ctx context.Context, workspaceID string) (string, error) {
	cipher, ok, err := s.store.GetConnectionToken(ctx, workspaceID, provider)
	if err != nil {
		return "", problem.Internalf(err, "load github token")
	}
	if !ok {
		return "", problem.NotFound("connect GitHub for this workspace first")
	}
	plain, err := s.box.Open(cipher)
	if err != nil {
		return "", problem.Internalf(err, "open github token")
	}
	return string(plain), nil
}

func (s *service) openState(cookie string) (oauthState, error) {
	if strings.TrimSpace(cookie) == "" {
		return oauthState{}, problem.InvalidInput("missing OAuth state; please try connecting again")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return oauthState{}, problem.InvalidInput("invalid OAuth state; please try connecting again")
	}
	plain, err := s.box.Open(sealed)
	if err != nil {
		return oauthState{}, problem.InvalidInput("invalid OAuth state; please try connecting again")
	}
	var st oauthState
	if err := json.Unmarshal(plain, &st); err != nil {
		return oauthState{}, problem.InvalidInput("invalid OAuth state; please try connecting again")
	}
	return st, nil
}

func toRepository(r github.RepoInfo) Repository {
	return Repository{
		Owner:         r.Owner,
		Name:          r.Name,
		FullName:      r.FullName,
		DefaultBranch: r.DefaultBranch,
		IsPrivate:     r.Private,
		HTMLURL:       r.HTMLURL,
		Description:   r.Description,
	}
}

// mapGitHubErr translates the github client's typed errors into plain-English domain
// errors. A missing and an inaccessible repo both surface as NotFound, so the message
// never reveals whether a private repo exists.
func mapGitHubErr(err error) error {
	switch {
	case errors.Is(err, github.ErrNotFound):
		return problem.NotFound("repository not found, or your GitHub access can't see it")
	case errors.Is(err, github.ErrUnauthorized):
		return problem.InvalidInput("GitHub rejected the request; the connection may have been revoked — reconnect GitHub")
	case errors.Is(err, github.ErrForbidden):
		return problem.PermissionDenied("GitHub denied access to this repository")
	case errors.Is(err, github.ErrRateLimited):
		return problem.Internalf(err, "GitHub rate limit reached; please try again shortly")
	default:
		return problem.Internalf(err, "could not reach GitHub")
	}
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as an internal
// error, so a domain error from the store/audit surfaces unchanged rather than masked.
func mapErr(err error, op string) error {
	if err == nil {
		return nil
	}
	var pe *problem.Error
	if errors.As(err, &pe) {
		return err
	}
	return problem.Internalf(err, "%s", op)
}

func newNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
