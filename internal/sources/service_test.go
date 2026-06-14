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
	testConnectionID = "33333333-3333-3333-3333-333333333333"
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
}

// newFakeStore has an active connection with a stored token and deletes successfully —
// tests override fields to exercise other paths.
func newFakeStore() *fakeStore {
	return &fakeStore{
		conn:          Connection{ID: testConnectionID, WorkspaceID: testWorkspaceID, Provider: provider, GitHubLogin: "octocat"},
		connOK:        true,
		tokenCipher:   []byte("sealed:gho_stored"),
		tokenOK:       true,
		deletedConnOK: true,
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
func (f *fakeStore) CountServicesByConnection(_ context.Context, _ string) (int64, error) {
	return f.countByConn, nil
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

// fakeBox seals by prefixing and opens by trimming, so the OAuth-state and token round-trips
// work and tests can assert the store received the sealer's output.
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

// fakeGitHub returns canned values or configured errors and records whether the repo-listing
// call (which needs a token) was made.
type fakeGitHub struct {
	branches        []string
	repos           []github.RepoInfo
	token           github.Token
	user            github.User
	reposErr        error
	branchErr       error
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
func (f *fakeGitHub) ListUserRepos(_ context.Context, _ string, _ github.ListReposOptions) ([]github.RepoInfo, error) {
	f.listReposCalled = true
	return f.repos, f.reposErr
}
func (f *fakeGitHub) ListBranches(_ context.Context, _, _, _ string) ([]string, error) {
	return f.branches, f.branchErr
}
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
	_, err := svc.CompleteGitHubAuth(authedCtxFor("user-2"), CompleteAuthInput{Code: "code", State: gh.authorizeState, CookieState: begin.State})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a rejected completion must not audit")
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
	store.tokenCipher = []byte("sealed:gho_live")
	gh := &fakeGitHub{}
	svc := newSvc(store, &fakeBox{}, gh, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	if err := svc.DisconnectGitHub(authedCtx(), testWorkspaceID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gh.revokedTokens) != 1 || gh.revokedTokens[0] != "gho_live" {
		t.Errorf("revoked = %v, want exactly [gho_live]", gh.revokedTokens)
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

func TestInvalidIDsRejected(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testOAuth(), fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.GetConnection(authedCtx(), "not-a-uuid"); !isKind(err, problem.KindInvalidInput) {
		t.Errorf("GetConnection bad id: got %v, want InvalidInput", err)
	}
	if _, err := svc.ListRepositories(authedCtx(), ListReposInput{WorkspaceID: "not-a-uuid"}); !isKind(err, problem.KindInvalidInput) {
		t.Errorf("ListRepositories bad id: got %v, want InvalidInput", err)
	}
}
