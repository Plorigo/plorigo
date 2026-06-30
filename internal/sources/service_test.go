package sources

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testWorkspaceID = "11111111-1111-1111-1111-111111111111"
	testConnID      = "22222222-2222-2222-2222-222222222222"
	testUserID      = "user-1"
)

// --- fakes ---

type fakeStore struct {
	conns         []Connection
	byID          map[string]Connection
	sealed        []byte
	sealedOK      bool
	insertedOAuth *OAuthConnectionWrite
	insertedApp   *AppConnectionWrite
	deletedOK     bool
	count         int64
}

func (f *fakeStore) ListConnectionsByWorkspace(_ context.Context, _ string) ([]Connection, error) {
	return f.conns, nil
}
func (f *fakeStore) GetConnectionByID(_ context.Context, id string) (Connection, bool, error) {
	c, ok := f.byID[id]
	return c, ok, nil
}
func (f *fakeStore) GetSealedTokenByConnection(_ context.Context, _ string) ([]byte, bool, error) {
	return f.sealed, f.sealedOK, nil
}
func (f *fakeStore) InsertOAuthConnection(_ context.Context, _ database.Tx, c OAuthConnectionWrite) (Connection, error) {
	f.insertedOAuth = &c
	return Connection{ID: "conn-oauth", WorkspaceID: c.WorkspaceID, Provider: c.Provider, Kind: kindOAuth, AccountLogin: c.AccountLogin}, nil
}
func (f *fakeStore) InsertAppConnection(_ context.Context, _ database.Tx, c AppConnectionWrite) (Connection, error) {
	f.insertedApp = &c
	iid := c.InstallationID
	return Connection{ID: "conn-app", WorkspaceID: c.WorkspaceID, Provider: c.Provider, Kind: kindApp, AccountLogin: c.AccountLogin, InstallationID: &iid}, nil
}
func (f *fakeStore) DeleteConnectionByID(_ context.Context, _ database.Tx, id string) (string, bool, error) {
	return id, f.deletedOK, nil
}
func (f *fakeStore) CountServicesByConnection(_ context.Context, _ string) (int64, error) {
	return f.count, nil
}

type fakeBox struct{}

func (fakeBox) Seal(pt []byte) ([]byte, error) { return append([]byte("sealed:"), pt...), nil }
func (fakeBox) Open(ct []byte) ([]byte, error) { return bytes.TrimPrefix(ct, []byte("sealed:")), nil }

type fakeRecorder struct{ action string }

func (f *fakeRecorder) Record(_ context.Context, _ database.Tx, action, _, _, _, _ string) error {
	f.action = action
	return nil
}

type fakeAuthz struct{ err error }

func (f fakeAuthz) Authorize(_ context.Context, _ principal.Principal, _ authz.Action, _ authz.Resource) error {
	return f.err
}

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

// fakeProvider implements Provider with canned values.
type fakeProvider struct {
	oauthConfigured bool
	appConfigured   bool
	account         Account
	repos           []Repo
	branches        []string
	repo            Repo
	token           string
}

func (f *fakeProvider) ID() string                         { return "github" }
func (f *fakeProvider) DisplayName() string                { return "GitHub" }
func (f *fakeProvider) OAuthConfigured() bool              { return f.oauthConfigured }
func (f *fakeProvider) AppConfigured(context.Context) bool { return f.appConfigured }
func (f *fakeProvider) AuthorizeURL(state string) string {
	return "https://gh.test/oauth?state=" + state
}
func (f *fakeProvider) ExchangeCode(_ context.Context, _ string) (string, string, Account, error) {
	return "tok", "repo", f.account, nil
}
func (f *fakeProvider) RevokeToken(context.Context, string) error { return nil }
func (f *fakeProvider) InstallURL(_ context.Context, state string) (string, bool) {
	if !f.appConfigured {
		return "", false
	}
	return "https://gh.test/apps/x/installations/new?state=" + state, true
}
func (f *fakeProvider) ResolveInstallation(context.Context, string) (Account, error) {
	return f.account, nil
}
func (f *fakeProvider) InstallationToken(context.Context, string) (string, error) {
	return f.token, nil
}
func (f *fakeProvider) ListRepos(context.Context, Conn, string, int) ([]Repo, error) {
	return f.repos, nil
}
func (f *fakeProvider) ListBranches(context.Context, Conn, string, string) ([]string, error) {
	return f.branches, nil
}
func (f *fakeProvider) GetRepository(context.Context, Conn, string, string) (Repo, error) {
	return f.repo, nil
}
func (f *fakeProvider) GetBranch(context.Context, Conn, string, string, string) error {
	return nil
}
func (f *fakeProvider) GetPullRequest(context.Context, Conn, string, string, int) (PullRequest, error) {
	return PullRequest{}, nil
}
func (f *fakeProvider) VerifyWebhook(string, []byte, string) bool { return true }
func (f *fakeProvider) Buildable(kind string) bool                { return kind == kindApp }

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: testUserID, Method: principal.MethodSession})
}

func newSvc(store Store, p *fakeProvider, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, fakeBox{}, NewRegistry(p), authorizer, rec, slog.Default())
}

func isKind(err error, kind problem.Kind) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == kind
}

func nonceFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u.Query().Get("state")
}

// --- tests ---

func TestListConnections_ReturnsConnsAndProviders(t *testing.T) {
	store := &fakeStore{conns: []Connection{{ID: testConnID, Provider: "github", Kind: kindApp, AccountLogin: "acme"}}}
	svc := newSvc(store, &fakeProvider{oauthConfigured: true, appConfigured: true}, fakeAuthz{}, &fakeRecorder{})
	res, err := svc.ListConnections(authedCtx(), testWorkspaceID)
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	if len(res.Connections) != 1 || res.Connections[0].AccountLogin != "acme" {
		t.Fatalf("connections = %+v", res.Connections)
	}
	if len(res.Providers) != 1 || res.Providers[0].Provider != "github" || !res.Providers[0].AppConfigured {
		t.Fatalf("providers = %+v", res.Providers)
	}
}

func TestBeginOAuth_NotConfigured(t *testing.T) {
	svc := newSvc(&fakeStore{}, &fakeProvider{oauthConfigured: false}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.BeginOAuth(authedCtx(), BeginConnectInput{WorkspaceID: testWorkspaceID, Provider: "github"})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput", err)
	}
}

func TestOAuthRoundTrip_InsertsConnection(t *testing.T) {
	store := &fakeStore{}
	rec := &fakeRecorder{}
	id := int64(42)
	p := &fakeProvider{oauthConfigured: true, account: Account{Login: "octocat", ID: &id}}
	svc := newSvc(store, p, fakeAuthz{}, rec)

	begin, err := svc.BeginOAuth(authedCtx(), BeginConnectInput{WorkspaceID: testWorkspaceID, Provider: "github"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	res, err := svc.CompleteOAuth(authedCtx(), CompleteOAuthInput{Provider: "github", Code: "code", State: nonceFromURL(t, begin.AuthorizeURL), CookieState: begin.State})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.AccountLogin != "octocat" {
		t.Errorf("account = %q", res.AccountLogin)
	}
	if store.insertedOAuth == nil || store.insertedOAuth.AccountLogin != "octocat" || string(store.insertedOAuth.TokenCipher) != "sealed:tok" {
		t.Errorf("inserted = %+v, want sealed token", store.insertedOAuth)
	}
	if rec.action != "source.oauth.connect" {
		t.Errorf("audit = %q", rec.action)
	}
}

func TestAppInstallRoundTrip_InsertsConnection(t *testing.T) {
	store := &fakeStore{}
	rec := &fakeRecorder{}
	p := &fakeProvider{appConfigured: true, account: Account{Login: "acme-org"}}
	svc := newSvc(store, p, fakeAuthz{}, rec)

	begin, err := svc.BeginAppInstall(authedCtx(), BeginConnectInput{WorkspaceID: testWorkspaceID, Provider: "github"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !strings.Contains(begin.AuthorizeURL, "installations/new") {
		t.Errorf("install url = %q", begin.AuthorizeURL)
	}
	_, err = svc.CompleteAppInstall(authedCtx(), CompleteAppInput{Provider: "github", InstallationID: "99", State: nonceFromURL(t, begin.AuthorizeURL), CookieState: begin.State})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if store.insertedApp == nil || store.insertedApp.InstallationID != "99" || store.insertedApp.AccountLogin != "acme-org" {
		t.Errorf("inserted = %+v", store.insertedApp)
	}
}

func TestDisconnectConnection_BlockedWhenInUse(t *testing.T) {
	store := &fakeStore{
		byID:  map[string]Connection{testConnID: {ID: testConnID, WorkspaceID: testWorkspaceID, Provider: "github", Kind: kindApp}},
		count: 2,
	}
	svc := newSvc(store, &fakeProvider{}, fakeAuthz{}, &fakeRecorder{})
	err := svc.DisconnectConnection(authedCtx(), testConnID)
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput (in use)", err)
	}
}

func TestDisconnectConnection_Deletes(t *testing.T) {
	store := &fakeStore{
		byID:      map[string]Connection{testConnID: {ID: testConnID, WorkspaceID: testWorkspaceID, Provider: "github", Kind: kindApp}},
		deletedOK: true,
	}
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeProvider{}, fakeAuthz{}, rec)
	if err := svc.DisconnectConnection(authedCtx(), testConnID); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if rec.action != "source.disconnect" {
		t.Errorf("audit = %q", rec.action)
	}
}

func TestListRepositories_ByConnection(t *testing.T) {
	store := &fakeStore{byID: map[string]Connection{testConnID: {ID: testConnID, WorkspaceID: testWorkspaceID, Provider: "github", Kind: kindApp, InstallationID: strptr("99")}}}
	p := &fakeProvider{token: "ghs_x", repos: []Repo{{FullName: "acme/api", Owner: "acme", Name: "api"}}}
	svc := newSvc(store, p, fakeAuthz{}, &fakeRecorder{})
	repos, err := svc.ListRepositories(authedCtx(), ListReposInput{ConnectionID: testConnID})
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "acme/api" {
		t.Fatalf("repos = %+v", repos)
	}
}

func TestValidateRepo_AppIsBuildable(t *testing.T) {
	store := &fakeStore{byID: map[string]Connection{testConnID: {ID: testConnID, WorkspaceID: testWorkspaceID, Provider: "github", Kind: kindApp, InstallationID: strptr("99"), AccountLogin: "acme"}}}
	p := &fakeProvider{token: "ghs_x", repo: Repo{Owner: "acme", Name: "api", FullName: "acme/api", DefaultBranch: "main", Private: true}}
	svc := newSvc(store, p, fakeAuthz{}, &fakeRecorder{})
	rr, err := svc.ValidateRepo(authedCtx(), testConnID, "acme", "api", "")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if rr.Branch != "main" || rr.Kind != kindApp || !rr.Buildable || !rr.IsPrivate {
		t.Fatalf("resolved = %+v, want buildable app repo on default branch", rr)
	}
}

func TestInstallationToken_OnlyForApp(t *testing.T) {
	store := &fakeStore{byID: map[string]Connection{
		"oauth-conn": {ID: "oauth-conn", WorkspaceID: testWorkspaceID, Provider: "github", Kind: kindOAuth},
		testConnID:   {ID: testConnID, WorkspaceID: testWorkspaceID, Provider: "github", Kind: kindApp, InstallationID: strptr("99")},
	}}
	svc := newSvc(store, &fakeProvider{token: "ghs_minted"}, fakeAuthz{}, &fakeRecorder{})

	if _, ok, _ := svc.InstallationToken(context.Background(), "oauth-conn"); ok {
		t.Error("oauth connection should not yield an installation token")
	}
	token, ok, err := svc.InstallationToken(context.Background(), testConnID)
	if err != nil || !ok || token != "ghs_minted" {
		t.Fatalf("app token = %q ok=%v err=%v", token, ok, err)
	}
}

func strptr(s string) *string { return &s }
