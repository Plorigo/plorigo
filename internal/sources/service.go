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
	app        AppConfig
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, box SecretBox, gh GitHubClient, oauth OAuthConfig, app AppConfig, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, box: box, gh: gh, oauth: oauth, app: app, authorizer: authorizer, audit: audit, log: log}
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
	_, appConnected, err := s.store.InstallationForWorkspace(ctx, workspaceID)
	if err != nil {
		return ConnectionStatus{}, problem.Internalf(err, "get connection")
	}
	return ConnectionStatus{
		Configured:    s.oauth.Configured(),
		Connected:     ok,
		Connection:    conn,
		AppConfigured: s.app.Configured(),
		AppConnected:  appConnected,
	}, nil
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
	// A connection still used by services must not be removed (a recovery path): the caller
	// changes or deletes those services first.
	count, err := s.store.CountServicesByConnection(ctx, conn.ID)
	if err != nil {
		return problem.Internalf(err, "disconnect github")
	}
	if count > 0 {
		return problem.InvalidInput("change or delete the %d service(s) using this GitHub connection first", count)
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
