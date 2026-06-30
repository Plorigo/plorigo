package sources

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// stateTTL bounds how long an in-flight connect handshake stays valid.
const stateTTL = 10 * time.Minute

// defaultProvider is used when a begin request omits the provider (current single-provider UI).
const defaultProvider = "github"

// service is the business logic. It orchestrates ports + the provider registry only — no SQL, no
// transport, no cryptography of its own (it seals/opens through SecretBox). Every mutation authorizes
// the caller before the WithinTx block and audits inside it (modules.md Rule 4). Provider network
// calls happen BEFORE the transaction. A token is sealed before storage and opened only to call the
// provider; it is NEVER returned by any method or logged.
type service struct {
	tx         TxRunner
	store      Store
	box        SecretBox
	providers  *Registry
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, box SecretBox, reg *Registry, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, box: box, providers: reg, authorizer: authorizer, audit: audit, log: log}
}

var _ Service = (*service)(nil)

// connectState is the sealed payload carried across a connect redirect. It binds the handshake to
// the workspace + user that started it and the provider, with a nonce echoed back as `state`.
type connectState struct {
	WorkspaceID string `json:"w"`
	UserID      string `json:"u"`
	Provider    string `json:"p"`
	Nonce       string `json:"n"`
	ExpiresAt   int64  `json:"e"`
}

// --- connect flows ---

func (s *service) BeginOAuth(ctx context.Context, in BeginConnectInput) (BeginAuthResult, error) {
	p, err := s.beginConnect(ctx, in)
	if err != nil {
		return BeginAuthResult{}, err
	}
	if !p.OAuthConfigured() {
		return BeginAuthResult{}, problem.InvalidInput("%s OAuth is not configured on this server", p.DisplayName())
	}
	sealed, nonce, err := s.sealState(ctx, in.WorkspaceID, p.ID())
	if err != nil {
		return BeginAuthResult{}, err
	}
	return BeginAuthResult{AuthorizeURL: p.AuthorizeURL(nonce), State: sealed}, nil
}

func (s *service) CompleteOAuth(ctx context.Context, in CompleteOAuthInput) (CompleteAuthResult, error) {
	st, p, err := s.openAndVerify(ctx, in.Provider, in.State, in.CookieState)
	if err != nil {
		return CompleteAuthResult{}, err
	}
	if strings.TrimSpace(in.Code) == "" {
		return CompleteAuthResult{}, problem.InvalidInput("missing authorization code")
	}
	token, scopes, account, err := p.ExchangeCode(ctx, in.Code)
	if err != nil {
		return CompleteAuthResult{}, mapProviderErr(err)
	}
	sealedToken, err := s.box.Seal([]byte(token))
	if err != nil {
		return CompleteAuthResult{}, problem.Internalf(err, "seal token")
	}
	userID := st.UserID
	var conn Connection
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		conn, txErr = s.store.InsertOAuthConnection(ctx, tx, OAuthConnectionWrite{
			WorkspaceID: st.WorkspaceID, Provider: p.ID(), AccountLogin: account.Login, AccountID: account.ID,
			TokenCipher: sealedToken, Scopes: scopes, ConnectedBy: &userID,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "source.oauth.connect", "source_connection", conn.ID, st.WorkspaceID, st.UserID)
	})
	if err != nil {
		return CompleteAuthResult{}, problem.Internalf(err, "connect oauth")
	}
	s.log.Info("oauth connected", "workspace_id", st.WorkspaceID, "provider", p.ID(), "account", account.Login, "actor", st.UserID)
	return CompleteAuthResult{WorkspaceID: st.WorkspaceID, AccountLogin: account.Login}, nil
}

func (s *service) BeginAppInstall(ctx context.Context, in BeginConnectInput) (BeginAuthResult, error) {
	p, err := s.beginConnect(ctx, in)
	if err != nil {
		return BeginAuthResult{}, err
	}
	if !p.AppConfigured(ctx) {
		return BeginAuthResult{}, problem.InvalidInput("the %s App is not configured on this server", p.DisplayName())
	}
	sealed, nonce, err := s.sealState(ctx, in.WorkspaceID, p.ID())
	if err != nil {
		return BeginAuthResult{}, err
	}
	installURL, ok := p.InstallURL(ctx, nonce)
	if !ok {
		return BeginAuthResult{}, problem.InvalidInput("the %s App is not configured on this server", p.DisplayName())
	}
	return BeginAuthResult{AuthorizeURL: installURL, State: sealed}, nil
}

func (s *service) CompleteAppInstall(ctx context.Context, in CompleteAppInput) (CompleteAuthResult, error) {
	if strings.TrimSpace(in.InstallationID) == "" {
		return CompleteAuthResult{}, problem.InvalidInput("missing installation id")
	}
	st, p, err := s.openAndVerify(ctx, in.Provider, in.State, in.CookieState)
	if err != nil {
		return CompleteAuthResult{}, err
	}
	account, err := p.ResolveInstallation(ctx, in.InstallationID)
	if err != nil {
		return CompleteAuthResult{}, mapProviderErr(err)
	}
	userID := st.UserID
	var conn Connection
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		var txErr error
		conn, txErr = s.store.InsertAppConnection(ctx, tx, AppConnectionWrite{
			WorkspaceID: st.WorkspaceID, Provider: p.ID(), AccountLogin: account.Login, AccountID: account.ID,
			InstallationID: in.InstallationID, ConnectedBy: &userID,
		})
		if txErr != nil {
			return txErr
		}
		return s.audit.Record(ctx, tx, "source.app.connect", "source_connection", conn.ID, st.WorkspaceID, st.UserID)
	})
	if err != nil {
		return CompleteAuthResult{}, problem.Internalf(err, "connect app")
	}
	s.log.Info("app installed", "workspace_id", st.WorkspaceID, "provider", p.ID(), "account", account.Login, "installation_id", in.InstallationID, "actor", st.UserID)
	return CompleteAuthResult{WorkspaceID: st.WorkspaceID, AccountLogin: account.Login}, nil
}

// --- RPC-backed reads/mutations ---

func (s *service) ListConnections(ctx context.Context, workspaceID string) (ListConnectionsResult, error) {
	if _, err := id.Parse(workspaceID); err != nil {
		return ListConnectionsResult{}, problem.InvalidInput("a valid workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceRead, authz.Resource{Type: "source", WorkspaceID: workspaceID}); err != nil {
		return ListConnectionsResult{}, err
	}
	conns, err := s.store.ListConnectionsByWorkspace(ctx, workspaceID)
	if err != nil {
		return ListConnectionsResult{}, problem.Internalf(err, "list connections")
	}
	return ListConnectionsResult{Connections: conns, Providers: s.providerStatuses(ctx)}, nil
}

func (s *service) DisconnectConnection(ctx context.Context, connectionID string) error {
	conn, ok, err := s.requireConnection(ctx, connectionID)
	if err != nil {
		return err
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceDisconnect, authz.Resource{Type: "source", WorkspaceID: conn.WorkspaceID}); err != nil {
		return err
	}
	_ = ok
	inUse, err := s.store.CountServicesByConnection(ctx, connectionID)
	if err != nil {
		return problem.Internalf(err, "disconnect")
	}
	if inUse > 0 {
		return problem.InvalidInput("this integration is used by %d service(s); remove or repoint them first", inUse)
	}
	// Best-effort token revocation at the provider (OAuth only) before deleting the local row.
	if conn.Kind == kindOAuth {
		if token, tok, terr := s.openToken(ctx, connectionID); terr == nil && tok {
			if p, pok := s.providers.Get(conn.Provider); pok {
				if rerr := p.RevokeToken(ctx, token); rerr != nil {
					s.log.Warn("provider token revoke failed (local connection still removed)", "provider", conn.Provider, "error", rerr)
				}
			}
		}
	}
	caller := principal.FromContext(ctx)
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		deletedID, deleted, txErr := s.store.DeleteConnectionByID(ctx, tx, connectionID)
		if txErr != nil {
			return txErr
		}
		if !deleted {
			return problem.NotFound("connection %s not found", connectionID)
		}
		return s.audit.Record(ctx, tx, "source.disconnect", "source_connection", deletedID, conn.WorkspaceID, caller.UserID)
	})
	if err != nil {
		var pe *problem.Error
		if errors.As(err, &pe) {
			return err
		}
		return problem.Internalf(err, "disconnect")
	}
	s.log.Info("integration disconnected", "connection_id", connectionID, "workspace_id", conn.WorkspaceID, "provider", conn.Provider, "actor", caller.UserID)
	return nil
}

func (s *service) ListRepositories(ctx context.Context, in ListReposInput) ([]Repository, error) {
	conn, _, err := s.requireConnection(ctx, in.ConnectionID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: conn.WorkspaceID}); err != nil {
		return nil, err
	}
	pc, p, err := s.resolveConn(ctx, conn)
	if err != nil {
		return nil, err
	}
	repos, err := p.ListRepos(ctx, pc, in.Query, in.Page)
	if err != nil {
		return nil, mapProviderErr(err)
	}
	out := make([]Repository, 0, len(repos))
	for _, r := range repos {
		out = append(out, Repository{
			Owner: r.Owner, Name: r.Name, FullName: r.FullName, DefaultBranch: r.DefaultBranch,
			IsPrivate: r.Private, HTMLURL: r.HTMLURL, Description: r.Description,
		})
	}
	return out, nil
}

func (s *service) ListBranches(ctx context.Context, connectionID, owner, repo string) ([]string, error) {
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
		return nil, problem.InvalidInput("owner and repo are required")
	}
	conn, _, err := s.requireConnection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: conn.WorkspaceID}); err != nil {
		return nil, err
	}
	pc, p, err := s.resolveConn(ctx, conn)
	if err != nil {
		return nil, err
	}
	branches, err := p.ListBranches(ctx, pc, owner, repo)
	if err != nil {
		return nil, mapProviderErr(err)
	}
	return branches, nil
}

// --- internal seams (not policy-authorized; callers authorize) ---

func (s *service) InstallationToken(ctx context.Context, connectionID string) (string, bool, error) {
	conn, ok, err := s.store.GetConnectionByID(ctx, connectionID)
	if err != nil {
		return "", false, problem.Internalf(err, "resolve connection")
	}
	if !ok || conn.Kind != kindApp || conn.InstallationID == nil || *conn.InstallationID == "" {
		return "", false, nil
	}
	p, pok := s.providers.Get(conn.Provider)
	if !pok {
		return "", false, nil
	}
	token, err := p.InstallationToken(ctx, *conn.InstallationID)
	if err != nil {
		return "", false, mapProviderErr(err)
	}
	return token, true, nil
}

func (s *service) GetConnectionMeta(ctx context.Context, connectionID string) (Connection, bool, error) {
	if _, err := id.Parse(connectionID); err != nil {
		return Connection{}, false, problem.InvalidInput("a valid connection_id is required")
	}
	conn, ok, err := s.store.GetConnectionByID(ctx, connectionID)
	if err != nil {
		return Connection{}, false, problem.Internalf(err, "resolve connection")
	}
	return conn, ok, nil
}

func (s *service) ValidateRepo(ctx context.Context, connectionID, owner, repo, branch string) (ResolvedRepo, error) {
	conn, _, err := s.requireConnection(ctx, connectionID)
	if err != nil {
		return ResolvedRepo{}, err
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" {
		return ResolvedRepo{}, problem.InvalidInput("owner and repo are required")
	}
	pc, p, err := s.resolveConn(ctx, conn)
	if err != nil {
		return ResolvedRepo{}, err
	}
	info, err := p.GetRepository(ctx, pc, strings.TrimSpace(owner), strings.TrimSpace(repo))
	if err != nil {
		return ResolvedRepo{}, mapProviderErr(err)
	}
	resolvedBranch := strings.TrimSpace(branch)
	if resolvedBranch == "" {
		resolvedBranch = info.DefaultBranch
	} else if berr := p.GetBranch(ctx, pc, info.Owner, info.Name, resolvedBranch); berr != nil {
		if errors.Is(berr, ErrNotFound) {
			return ResolvedRepo{}, problem.InvalidInput("branch %q was not found in %s", resolvedBranch, info.FullName)
		}
		return ResolvedRepo{}, mapProviderErr(berr)
	}
	return ResolvedRepo{
		Owner: info.Owner, Name: info.Name, FullName: info.FullName, DefaultBranch: info.DefaultBranch,
		HTMLURL: info.HTMLURL, IsPrivate: info.Private, Branch: resolvedBranch,
		Provider: conn.Provider, Kind: conn.Kind, AccountLogin: conn.AccountLogin, Buildable: p.Buildable(conn.Kind),
	}, nil
}

// --- helpers ---

// beginConnect validates the workspace, authorizes ActionSourceConnect, and resolves the provider.
func (s *service) beginConnect(ctx context.Context, in BeginConnectInput) (Provider, error) {
	if _, err := id.Parse(in.WorkspaceID); err != nil {
		return nil, problem.InvalidInput("a valid workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: in.WorkspaceID}); err != nil {
		return nil, err
	}
	return s.provider(in.Provider)
}

// openAndVerify opens a sealed connect state, verifies the nonce/expiry/user/provider, re-authorizes,
// and returns the state + resolved provider.
func (s *service) openAndVerify(ctx context.Context, routeProvider, echoedNonce, cookieState string) (connectState, Provider, error) {
	st, err := s.openState(cookieState)
	if err != nil {
		return connectState{}, nil, err
	}
	if echoedNonce == "" || st.Nonce != echoedNonce {
		return connectState{}, nil, problem.InvalidInput("connect state mismatch; please try again")
	}
	if time.Now().Unix() > st.ExpiresAt {
		return connectState{}, nil, problem.InvalidInput("the connect request expired; please try again")
	}
	if routeProvider != "" && routeProvider != st.Provider {
		return connectState{}, nil, problem.InvalidInput("connect provider mismatch; please try again")
	}
	caller := principal.FromContext(ctx)
	if !caller.IsAuthenticated() || caller.UserID != st.UserID {
		return connectState{}, nil, problem.PermissionDenied("this connect flow was started by a different session")
	}
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionSourceConnect, authz.Resource{Type: "source", WorkspaceID: st.WorkspaceID}); err != nil {
		return connectState{}, nil, err
	}
	p, err := s.provider(st.Provider)
	if err != nil {
		return connectState{}, nil, err
	}
	return st, p, nil
}

func (s *service) provider(idStr string) (Provider, error) {
	if strings.TrimSpace(idStr) == "" {
		idStr = defaultProvider
	}
	p, ok := s.providers.Get(idStr)
	if !ok {
		return nil, problem.InvalidInput("unknown provider %q", idStr)
	}
	return p, nil
}

// requireConnection loads a connection by id, returning NotFound when missing.
func (s *service) requireConnection(ctx context.Context, connectionID string) (Connection, bool, error) {
	if _, err := id.Parse(connectionID); err != nil {
		return Connection{}, false, problem.InvalidInput("a valid connection_id is required")
	}
	conn, ok, err := s.store.GetConnectionByID(ctx, connectionID)
	if err != nil {
		return Connection{}, false, problem.Internalf(err, "resolve connection")
	}
	if !ok {
		return Connection{}, false, problem.NotFound("connection %s not found", connectionID)
	}
	return conn, true, nil
}

// resolveConn turns a connection into a provider credential: an opened OAuth token, or a freshly
// minted App installation token.
func (s *service) resolveConn(ctx context.Context, conn Connection) (Conn, Provider, error) {
	p, ok := s.providers.Get(conn.Provider)
	if !ok {
		return Conn{}, nil, problem.InvalidInput("unknown provider %q", conn.Provider)
	}
	pc := Conn{Kind: conn.Kind}
	if conn.Kind == kindApp {
		if conn.InstallationID == nil || *conn.InstallationID == "" {
			return Conn{}, nil, problem.Internalf(errors.New("app connection missing installation id"), "resolve connection")
		}
		pc.InstallationID = *conn.InstallationID
		token, err := p.InstallationToken(ctx, *conn.InstallationID)
		if err != nil {
			return Conn{}, nil, mapProviderErr(err)
		}
		pc.Token = token
		return pc, p, nil
	}
	token, tok, err := s.openToken(ctx, conn.ID)
	if err != nil {
		return Conn{}, nil, err
	}
	if !tok {
		return Conn{}, nil, problem.NotFound("this integration has no stored credential; reconnect it")
	}
	pc.Token = token
	return pc, p, nil
}

// openToken loads + opens a connection's sealed OAuth token. ok is false when none is stored.
func (s *service) openToken(ctx context.Context, connectionID string) (string, bool, error) {
	cipher, ok, err := s.store.GetSealedTokenByConnection(ctx, connectionID)
	if err != nil {
		return "", false, problem.Internalf(err, "load token")
	}
	if !ok {
		return "", false, nil
	}
	plain, err := s.box.Open(cipher)
	if err != nil {
		return "", false, problem.Internalf(err, "open token")
	}
	return string(plain), true, nil
}

func (s *service) providerStatuses(ctx context.Context) []ProviderStatus {
	all := s.providers.All()
	out := make([]ProviderStatus, 0, len(all))
	for _, p := range all {
		out = append(out, ProviderStatus{
			Provider:        p.ID(),
			DisplayName:     p.DisplayName(),
			OAuthConfigured: p.OAuthConfigured(),
			AppConfigured:   p.AppConfigured(ctx),
			Available:       true,
		})
	}
	return out
}

func (s *service) sealState(ctx context.Context, workspaceID, providerID string) (sealed, nonce string, err error) {
	nonce, err = newNonce()
	if err != nil {
		return "", "", problem.Internalf(err, "begin connect")
	}
	payload, err := json.Marshal(connectState{
		WorkspaceID: workspaceID, UserID: principal.FromContext(ctx).UserID, Provider: providerID,
		Nonce: nonce, ExpiresAt: time.Now().Add(stateTTL).Unix(),
	})
	if err != nil {
		return "", "", problem.Internalf(err, "begin connect")
	}
	b, err := s.box.Seal(payload)
	if err != nil {
		return "", "", problem.Internalf(err, "seal state")
	}
	return base64.RawURLEncoding.EncodeToString(b), nonce, nil
}

func (s *service) openState(cookie string) (connectState, error) {
	if strings.TrimSpace(cookie) == "" {
		return connectState{}, problem.InvalidInput("missing connect state; please try again")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return connectState{}, problem.InvalidInput("invalid connect state; please try again")
	}
	plain, err := s.box.Open(sealed)
	if err != nil {
		return connectState{}, problem.InvalidInput("invalid connect state; please try again")
	}
	var st connectState
	if err := json.Unmarshal(plain, &st); err != nil {
		return connectState{}, problem.InvalidInput("invalid connect state; please try again")
	}
	return st, nil
}

func newNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func mapProviderErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return problem.NotFound("repository not found or not accessible by this integration")
	case errors.Is(err, ErrUnauthorized):
		return problem.PermissionDenied("the provider rejected the request; reconnect the integration")
	case errors.Is(err, ErrForbidden):
		return problem.PermissionDenied("the provider denied access")
	case errors.Is(err, ErrRateLimited):
		return problem.Internalf(err, "the provider is rate limiting requests; try again shortly")
	default:
		return problem.Internalf(err, "provider request failed")
	}
}
