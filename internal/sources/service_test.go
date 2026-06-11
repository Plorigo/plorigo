package sources

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testWorkspaceID  = "11111111-1111-1111-1111-111111111111"
	testProjectID    = "22222222-2222-2222-2222-222222222222"
	testConnectionID = "33333333-3333-3333-3333-333333333333"
	testSourceID     = "44444444-4444-4444-4444-444444444444"
	testUserID       = "user-1"
)

type fakeStore struct {
	conn          Connection
	connOK        bool
	tokenCipher   []byte
	tokenOK       bool
	upsertedConn  *ConnectionWrite
	deletedConnOK bool
	countByConn   int64

	upsertedSource *ProjectSourceWrite
	src            Source
	srcOK          bool
	list           []Source
	deletedSrcOK   bool

	wsID string
	wsOK bool
}

// newFakeStore resolves the project to testWorkspaceID, has an active connection with a
// stored token, and deletes successfully — tests override fields to exercise other paths.
func newFakeStore() *fakeStore {
	return &fakeStore{
		conn:          Connection{ID: testConnectionID, WorkspaceID: testWorkspaceID, Provider: provider, GitHubLogin: "octocat"},
		connOK:        true,
		tokenCipher:   []byte("sealed:gho_stored"),
		tokenOK:       true,
		deletedConnOK: true,
		deletedSrcOK:  true,
		wsID:          testWorkspaceID,
		wsOK:          true,
	}
}

func (f *fakeStore) UpsertConnection(_ context.Context, _ database.Tx, c ConnectionWrite) (Connection, error) {
	f.upsertedConn = &c
	return Connection{ID: testConnectionID, WorkspaceID: c.WorkspaceID, Provider: c.Provider, GitHubLogin: c.GitHubLogin}, nil
}
func (f *fakeStore) GetConnection(_ context.Context, _, _ string) (Connection, bool, error) {
	return f.conn, f.connOK, nil
}
func (f *fakeStore) GetConnectionToken(_ context.Context, _, _ string) ([]byte, bool, error) {
	return f.tokenCipher, f.tokenOK, nil
}
func (f *fakeStore) DeleteConnection(_ context.Context, _ database.Tx, _, _ string) (string, bool, error) {
	return testConnectionID, f.deletedConnOK, nil
}
func (f *fakeStore) CountProjectSourcesByConnection(_ context.Context, _ string) (int64, error) {
	return f.countByConn, nil
}
func (f *fakeStore) UpsertProjectSource(_ context.Context, _ database.Tx, w ProjectSourceWrite) (Source, error) {
	f.upsertedSource = &w
	return Source{ID: testSourceID, ProjectID: w.ProjectID, ConnectionID: w.ConnectionID, Provider: w.Provider,
		Owner: w.Owner, Repo: w.Repo, FullName: w.FullName, Branch: w.Branch, DefaultBranch: w.DefaultBranch,
		IsPrivate: w.IsPrivate, HTMLURL: w.HTMLURL}, nil
}
func (f *fakeStore) GetProjectSource(_ context.Context, _ string) (Source, bool, error) {
	return f.src, f.srcOK, nil
}
func (f *fakeStore) ListByWorkspace(_ context.Context, _ string) ([]Source, error) {
	return f.list, nil
}
func (f *fakeStore) DeleteProjectSource(_ context.Context, _ database.Tx, _ string) (string, bool, error) {
	return testSourceID, f.deletedSrcOK, nil
}
func (f *fakeStore) WorkspaceIDForProject(_ context.Context, _ string) (string, bool, error) {
	return f.wsID, f.wsOK, nil
}

type fakeRecorder struct {
	called bool
	action string
}

func (f *fakeRecorder) Record(_ context.Context, _ database.Tx, action, _, _, _, _ string) error {
	f.called = true
	f.action = action
	return nil
}

type fakeAuthz struct{ err error }

func (f fakeAuthz) Authorize(_ context.Context, _ principal.Principal, _ authz.Action, _ authz.Resource) error {
	return f.err
}

// fakeBox seals by prefixing and opens by trimming, so the OAuth-state and token
// round-trips work and tests can assert the store received the sealer's output.
type fakeBox struct{ sealErr error }

func (f *fakeBox) Seal(pt []byte) ([]byte, error) {
	if f.sealErr != nil {
		return nil, f.sealErr
	}
	return append([]byte("sealed:"), pt...), nil
}
func (f *fakeBox) Open(ct []byte) ([]byte, error) {
	return bytes.TrimPrefix(ct, []byte("sealed:")), nil
}

// fakeGitHub returns canned values or configured errors and records whether the
// repo-listing call (which needs a token) was made.
type fakeGitHub struct {
	repo            github.RepoInfo
	branches        []string
	repos           []github.RepoInfo
	token           github.Token
	user            github.User
	repoErr         error
	branchErr       error
	getBranchErr    error
	reposErr        error
	exchangeErr     error
	userErr         error
	revokeErr       error
	authorizeState  string
	listReposCalled bool
	revokedTokens   []string
}

func (f *fakeGitHub) AuthorizeURL(_, _, _, state string) string {
	f.authorizeState = state
	return "https://github.test/login/oauth/authorize?state=" + state
}
func (f *fakeGitHub) ExchangeCode(_ context.Context, _, _, _, _ string) (github.Token, error) {
	return f.token, f.exchangeErr
}
func (f *fakeGitHub) GetAuthenticatedUser(_ context.Context, _ string) (github.User, error) {
	return f.user, f.userErr
}
func (f *fakeGitHub) GetRepository(_ context.Context, _, _, _ string) (github.RepoInfo, error) {
	return f.repo, f.repoErr
}
func (f *fakeGitHub) ListUserRepos(_ context.Context, _ string, _ github.ListReposOptions) ([]github.RepoInfo, error) {
	f.listReposCalled = true
	return f.repos, f.reposErr
}
func (f *fakeGitHub) ListBranches(_ context.Context, _, _, _ string) ([]string, error) {
	return f.branches, f.branchErr
}
func (f *fakeGitHub) GetBranch(_ context.Context, _, _, _, _ string) error { return f.getBranchErr }
func (f *fakeGitHub) RevokeToken(_ context.Context, _, _, token string) error {
	f.revokedTokens = append(f.revokedTokens, token)
	return f.revokeErr
}

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func authedCtx() context.Context { return authedCtxFor(testUserID) }

func authedCtxFor(userID string) context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: userID, Method: principal.MethodSession})
}

func testOAuth() OAuthConfig {
	return OAuthConfig{ClientID: "cid", ClientSecret: "csec", Scopes: "repo", RedirectURL: "https://app/api/github/callback"}
}

func newSvc(store Store, box SecretBox, gh GitHubClient, oauth OAuthConfig, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, box, gh, oauth, authorizer, rec, slog.Default())
}

func isKind(err error, kind problem.Kind) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == kind
}

// --- OAuth begin / complete ---

func TestBeginGitHubAuth_NotConfigured(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeBox{}, &fakeGitHub{}, OAuthConfig{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.BeginGitHubAuth(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput", err)
	}
}

func TestBeginGitHubAuth_DeniedWhenUnauthorized(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.BeginGitHubAuth(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
}

func TestCompleteGitHubAuth_SealsTokenAndAudits(t *testing.T) {
	store := newFakeStore()
	store.tokenOK = false // a clean first-time connection (no prior token to revoke)
	gh := &fakeGitHub{token: github.Token{AccessToken: "gho_secret_abc", Scope: "repo"}, user: github.User{Login: "octocat", ID: 42}}
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, rec)

	begin, err := svc.BeginGitHubAuth(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	res, err := svc.CompleteGitHubAuth(authedCtx(), CompleteAuthInput{Code: "code", State: gh.authorizeState, CookieState: begin.State})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.GitHubLogin != "octocat" || res.WorkspaceID != testWorkspaceID {
		t.Fatalf("unexpected result: %+v", res)
	}
	if store.upsertedConn == nil {
		t.Fatal("connection was not upserted")
	}
	if string(store.upsertedConn.TokenCiphertext) != "sealed:gho_secret_abc" {
		t.Errorf("stored token = %q, want the sealer's output", store.upsertedConn.TokenCiphertext)
	}
	if !rec.called || rec.action != "source.github.connect" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
	if len(gh.revokedTokens) != 0 {
		t.Errorf("first connect must not revoke any token, revoked: %v", gh.revokedTokens)
	}
}

func TestCompleteGitHubAuth_RevokesSupersededToken(t *testing.T) {
	store := newFakeStore()
	store.tokenCipher = []byte("sealed:old_token") // a prior connection exists
	gh := &fakeGitHub{token: github.Token{AccessToken: "new_token", Scope: "repo"}, user: github.User{Login: "octocat", ID: 42}}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})

	begin, _ := svc.BeginGitHubAuth(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if _, err := svc.CompleteGitHubAuth(authedCtx(), CompleteAuthInput{Code: "code", State: gh.authorizeState, CookieState: begin.State}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(gh.revokedTokens) != 1 || gh.revokedTokens[0] != "old_token" {
		t.Errorf("revoked = %v, want exactly [old_token]", gh.revokedTokens)
	}
}

func TestCompleteGitHubAuth_SameTokenNotRevoked(t *testing.T) {
	store := newFakeStore()
	store.tokenCipher = []byte("sealed:same_token")
	// GitHub returned the same token on re-auth — revoking it would kill the live one.
	gh := &fakeGitHub{token: github.Token{AccessToken: "same_token", Scope: "repo"}, user: github.User{Login: "octocat", ID: 42}}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})

	begin, _ := svc.BeginGitHubAuth(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if _, err := svc.CompleteGitHubAuth(authedCtx(), CompleteAuthInput{Code: "code", State: gh.authorizeState, CookieState: begin.State}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(gh.revokedTokens) != 0 {
		t.Errorf("an unchanged token must not be revoked, revoked: %v", gh.revokedTokens)
	}
}

func TestCompleteGitHubAuth_StateMismatch(t *testing.T) {
	gh := &fakeGitHub{token: github.Token{AccessToken: "t"}, user: github.User{Login: "x"}}
	svc := newSvc(newFakeStore(), &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	begin, _ := svc.BeginGitHubAuth(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	_, err := svc.CompleteGitHubAuth(authedCtx(), CompleteAuthInput{Code: "code", State: "wrong-nonce", CookieState: begin.State})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput", err)
	}
}

func TestCompleteGitHubAuth_DifferentUserRejected(t *testing.T) {
	gh := &fakeGitHub{token: github.Token{AccessToken: "t"}, user: github.User{Login: "x"}}
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), &fakeBox{}, gh, testOAuth(), fakeAuthz{}, rec)
	begin, _ := svc.BeginGitHubAuth(authedCtxFor("user-1"), BeginAuthInput{WorkspaceID: testWorkspaceID})
	// A different session completes the handshake.
	_, err := svc.CompleteGitHubAuth(authedCtxFor("user-2"), CompleteAuthInput{Code: "code", State: gh.authorizeState, CookieState: begin.State})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a rejected completion must not audit")
	}
}

func TestCompleteGitHubAuth_InvalidStateCookie(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CompleteGitHubAuth(authedCtx(), CompleteAuthInput{Code: "code", State: "n", CookieState: "!!!not-base64!!!"})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput", err)
	}
}

// --- connection reads / disconnect ---

func TestGetConnection_ReportsState(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store, &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	st, err := svc.GetConnection(authedCtx(), testWorkspaceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !st.Configured || !st.Connected || st.Connection.GitHubLogin != "octocat" {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestDisconnectGitHub_BlockedWhenInUse(t *testing.T) {
	store := newFakeStore()
	store.countByConn = 2
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, rec)
	err := svc.DisconnectGitHub(authedCtx(), testWorkspaceID)
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput", err)
	}
	if rec.called {
		t.Error("a blocked disconnect must not audit")
	}
}

func TestDisconnectGitHub_AuditsOnSuccess(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, rec)
	if err := svc.DisconnectGitHub(authedCtx(), testWorkspaceID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rec.called || rec.action != "source.github.disconnect" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestDisconnectGitHub_RevokesToken(t *testing.T) {
	store := newFakeStore()
	store.tokenCipher = []byte("sealed:gho_live") // the token currently stored
	gh := &fakeGitHub{}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	if err := svc.DisconnectGitHub(authedCtx(), testWorkspaceID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gh.revokedTokens) != 1 || gh.revokedTokens[0] != "gho_live" {
		t.Errorf("revoked = %v, want exactly [gho_live]", gh.revokedTokens)
	}
}

func TestDisconnectGitHub_RevokeFailureDoesNotFail(t *testing.T) {
	store := newFakeStore()
	rec := &fakeRecorder{}
	gh := &fakeGitHub{revokeErr: errors.New("github down")}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, rec)
	// A failed revoke is logged and swallowed — the disconnect still succeeds and audits.
	if err := svc.DisconnectGitHub(authedCtx(), testWorkspaceID); err != nil {
		t.Fatalf("disconnect must succeed even if revoke fails: %v", err)
	}
	if !rec.called {
		t.Error("disconnect should still audit even when revoke fails")
	}
}

// --- repository listing ---

func TestListRepositories_DeniedDoesNotCallGitHub(t *testing.T) {
	gh := &fakeGitHub{}
	svc := newSvc(newFakeStore(), &fakeBox{}, gh, testOAuth(), fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.ListRepositories(authedCtx(), ListReposInput{WorkspaceID: testWorkspaceID})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
	if gh.listReposCalled {
		t.Error("a denied list must not reach GitHub (no token oracle)")
	}
}

func TestListRepositories_FiltersByQuery(t *testing.T) {
	store := newFakeStore()
	gh := &fakeGitHub{repos: []github.RepoInfo{
		{FullName: "octocat/hello"}, {FullName: "octocat/world"}, {FullName: "octocat/help"},
	}}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	repos, err := svc.ListRepositories(authedCtx(), ListReposInput{WorkspaceID: testWorkspaceID, Query: "hel"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("len = %d, want 2 (hello, help)", len(repos))
	}
}

func TestListRepositories_NoConnection(t *testing.T) {
	store := newFakeStore()
	store.tokenOK = false
	svc := newSvc(store, &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.ListRepositories(authedCtx(), ListReposInput{WorkspaceID: testWorkspaceID})
	if !isKind(err, problem.KindNotFound) {
		t.Fatalf("got %v, want NotFound", err)
	}
}

// --- connect repository ---

func TestConnectRepository_Success(t *testing.T) {
	store := newFakeStore()
	gh := &fakeGitHub{
		repo:     github.RepoInfo{Owner: "octocat", Name: "Hello-World", FullName: "octocat/Hello-World", DefaultBranch: "main", Private: true, HTMLURL: "https://github.com/octocat/Hello-World"},
		branches: []string{"main", "dev"},
	}
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, rec)

	// Client supplies lowercase casing; the stored metadata must come from GitHub.
	src, err := svc.ConnectRepository(authedCtx(), ConnectRepoInput{ProjectID: testProjectID, Owner: "octocat", Repo: "hello-world", Branch: "dev"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.upsertedSource == nil {
		t.Fatal("source was not upserted")
	}
	if store.upsertedSource.FullName != "octocat/Hello-World" || store.upsertedSource.Repo != "Hello-World" {
		t.Errorf("stored metadata not taken from GitHub: %+v", store.upsertedSource)
	}
	if !store.upsertedSource.IsPrivate || store.upsertedSource.DefaultBranch != "main" {
		t.Errorf("authoritative metadata missing: %+v", store.upsertedSource)
	}
	if store.upsertedSource.Branch != "dev" || store.upsertedSource.ConnectionID != testConnectionID {
		t.Errorf("unexpected source write: %+v", store.upsertedSource)
	}
	if src.WorkspaceID != testWorkspaceID || src.GitHubLogin != "octocat" {
		t.Errorf("returned source missing resolved fields: %+v", src)
	}
	if !rec.called || rec.action != "source.connect" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestConnectRepository_BranchNotFound(t *testing.T) {
	store := newFakeStore()
	gh := &fakeGitHub{repo: github.RepoInfo{Owner: "o", Name: "r", FullName: "o/r"}, getBranchErr: github.ErrNotFound}
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, rec)
	_, err := svc.ConnectRepository(authedCtx(), ConnectRepoInput{ProjectID: testProjectID, Owner: "o", Repo: "r", Branch: "nope"})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput", err)
	}
	if store.upsertedSource != nil || rec.called {
		t.Error("a bad branch must not upsert or audit")
	}
}

func TestConnectRepository_NoConnection(t *testing.T) {
	store := newFakeStore()
	store.connOK = false
	gh := &fakeGitHub{}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.ConnectRepository(authedCtx(), ConnectRepoInput{ProjectID: testProjectID, Owner: "o", Repo: "r", Branch: "main"})
	if !isKind(err, problem.KindNotFound) {
		t.Fatalf("got %v, want NotFound", err)
	}
}

func TestConnectRepository_ProjectNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false
	svc := newSvc(store, &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.ConnectRepository(authedCtx(), ConnectRepoInput{ProjectID: testProjectID, Owner: "o", Repo: "r", Branch: "main"})
	if !isKind(err, problem.KindNotFound) {
		t.Fatalf("got %v, want NotFound", err)
	}
}

func TestConnectRepository_DeniedDoesNotCallGitHub(t *testing.T) {
	store := newFakeStore()
	gh := &fakeGitHub{}
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	_, err := svc.ConnectRepository(authedCtx(), ConnectRepoInput{ProjectID: testProjectID, Owner: "o", Repo: "r", Branch: "main"})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
	if store.upsertedSource != nil || rec.called {
		t.Error("a denied connect must not upsert or audit")
	}
}

func TestConnectRepository_RepoNotFoundMapsToNotFound(t *testing.T) {
	store := newFakeStore()
	gh := &fakeGitHub{repoErr: github.ErrNotFound}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.ConnectRepository(authedCtx(), ConnectRepoInput{ProjectID: testProjectID, Owner: "o", Repo: "r", Branch: "main"})
	if !isKind(err, problem.KindNotFound) {
		t.Fatalf("got %v, want NotFound", err)
	}
}

func TestConnectRepository_RevokedTokenMapsToInvalidInput(t *testing.T) {
	store := newFakeStore()
	gh := &fakeGitHub{repoErr: github.ErrUnauthorized}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.ConnectRepository(authedCtx(), ConnectRepoInput{ProjectID: testProjectID, Owner: "o", Repo: "r", Branch: "main"})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput", err)
	}
}

// --- project source reads / disconnect ---

func TestGetProjectSource_NotFound(t *testing.T) {
	store := newFakeStore()
	store.srcOK = false
	svc := newSvc(store, &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.GetProjectSource(authedCtx(), testProjectID)
	if !isKind(err, problem.KindNotFound) {
		t.Fatalf("got %v, want NotFound", err)
	}
}

func TestGetProjectSource_Success(t *testing.T) {
	store := newFakeStore()
	store.src = Source{ID: testSourceID, ProjectID: testProjectID, FullName: "o/r", Branch: "main", GitHubLogin: "octocat"}
	store.srcOK = true
	svc := newSvc(store, &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	src, err := svc.GetProjectSource(authedCtx(), testProjectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src.WorkspaceID != testWorkspaceID || src.FullName != "o/r" {
		t.Fatalf("unexpected source: %+v", src)
	}
}

func TestListByWorkspace_DeniedWhenUnauthorized(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.ListByWorkspace(authedCtx(), testWorkspaceID)
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
}

func TestDisconnectRepository_AuditsOnSuccess(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, rec)
	if err := svc.DisconnectRepository(authedCtx(), testProjectID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rec.called || rec.action != "source.disconnect" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestDisconnectRepository_NotFoundWhenNothingDeleted(t *testing.T) {
	store := newFakeStore()
	store.deletedSrcOK = false
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, rec)
	err := svc.DisconnectRepository(authedCtx(), testProjectID)
	if !isKind(err, problem.KindNotFound) {
		t.Fatalf("got %v, want NotFound", err)
	}
	if rec.called {
		t.Error("a delete that removed nothing must not audit")
	}
}

func TestInvalidIDsRejected(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.GetConnection(authedCtx(), "not-a-uuid"); !isKind(err, problem.KindInvalidInput) {
		t.Errorf("GetConnection bad id: got %v, want InvalidInput", err)
	}
	if _, err := svc.GetProjectSource(authedCtx(), "not-a-uuid"); !isKind(err, problem.KindInvalidInput) {
		t.Errorf("GetProjectSource bad id: got %v, want InvalidInput", err)
	}
	if _, err := svc.ConnectRepository(authedCtx(), ConnectRepoInput{ProjectID: "bad", Owner: "o", Repo: "r", Branch: "m"}); !isKind(err, problem.KindInvalidInput) {
		t.Errorf("ConnectRepository bad id: got %v, want InvalidInput", err)
	}
}
