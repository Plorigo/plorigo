package services

import (
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
	testEnvID     = "11111111-1111-1111-1111-111111111111"
	testProjectID = "33333333-3333-3333-3333-333333333333"
	testServerID  = "22222222-2222-2222-2222-222222222222"
	testServiceID = "00000000-0000-0000-0000-0000000000aa"
	testWorkspace = "ws-1"
	testDepID     = "44444444-4444-4444-4444-444444444444"
)

type fakeStore struct {
	envWs, envProj string
	envOK          bool

	svcWs, svcProj string
	svcMetaOK      bool

	serverWs string
	serverOK bool

	projWs string
	projOK bool

	connID, connLogin string
	connOK            bool
	token             []byte
	tokenOK           bool

	appConnID, appInstallID string
	appOK                   bool

	insertedImage ServiceWrite
	insertedGit   GitServiceWrite
	insertErr     error

	getSvc Service
	getOK  bool
}

func (f *fakeStore) InsertService(_ context.Context, _ database.Tx, w ServiceWrite) (Service, error) {
	f.insertedImage = w
	if f.insertErr != nil {
		return Service{}, f.insertErr
	}
	return Service{ID: testServiceID, EnvironmentID: w.EnvironmentID, ProjectID: w.ProjectID, WorkspaceID: w.WorkspaceID, Name: w.Name, Slug: w.Slug, SourceKind: w.SourceKind, ImageRef: w.ImageRef, ContainerPort: w.ContainerPort, Visibility: w.Visibility}, nil
}
func (f *fakeStore) InsertGitService(_ context.Context, _ database.Tx, w GitServiceWrite) (Service, error) {
	f.insertedGit = w
	if f.insertErr != nil {
		return Service{}, f.insertErr
	}
	return Service{ID: testServiceID, EnvironmentID: w.EnvironmentID, ProjectID: w.ProjectID, WorkspaceID: w.WorkspaceID, Name: w.Name, Slug: w.Slug, SourceKind: SourceGit, SourceAccess: w.SourceAccess, ConnectionID: w.ConnectionID, Owner: w.Owner, Repo: w.Repo, Branch: w.Branch, ContainerPort: w.ContainerPort, Visibility: w.Visibility}, nil
}
func (f *fakeStore) GetService(_ context.Context, _ string) (Service, bool, error) {
	return f.getSvc, f.getOK, nil
}
func (f *fakeStore) ListByEnvironment(_ context.Context, _ string) ([]Service, error) {
	return nil, nil
}
func (f *fakeStore) ListByProject(_ context.Context, _ string) ([]Service, error)   { return nil, nil }
func (f *fakeStore) ListByWorkspace(_ context.Context, _ string) ([]Service, error) { return nil, nil }
func (f *fakeStore) UpdateServiceSource(_ context.Context, _ database.Tx, w SourceWrite) (Service, error) {
	return Service{ID: w.ID, SourceKind: w.SourceKind, ImageRef: w.ImageRef, ContainerPort: w.ContainerPort}, nil
}
func (f *fakeStore) UpdateVisibility(_ context.Context, _ database.Tx, id, visibility string) (Service, error) {
	return Service{ID: id, Visibility: visibility}, nil
}
func (f *fakeStore) DeleteService(_ context.Context, _ database.Tx, id string) (string, bool, error) {
	return id, f.getOK, nil
}
func (f *fakeStore) WorkspaceAndProjectForEnvironment(_ context.Context, _ string) (string, string, bool, error) {
	return f.envWs, f.envProj, f.envOK, nil
}
func (f *fakeStore) WorkspaceAndProjectForService(_ context.Context, _ string) (string, string, bool, error) {
	return f.svcWs, f.svcProj, f.svcMetaOK, nil
}
func (f *fakeStore) WorkspaceForServer(_ context.Context, _ string) (string, bool, error) {
	return f.serverWs, f.serverOK, nil
}
func (f *fakeStore) WorkspaceForProject(_ context.Context, _ string) (string, bool, error) {
	return f.projWs, f.projOK, nil
}
func (f *fakeStore) GetConnection(_ context.Context, _ string) (string, string, bool, error) {
	return f.connID, f.connLogin, f.connOK, nil
}
func (f *fakeStore) GetConnectionToken(_ context.Context, _ string) ([]byte, bool, error) {
	return f.token, f.tokenOK, nil
}
func (f *fakeStore) GetAppInstallation(_ context.Context, _ string) (string, string, bool, error) {
	return f.appConnID, f.appInstallID, f.appOK, nil
}

type fakeEnqueuer struct {
	called    bool
	serviceID string
}

func (f *fakeEnqueuer) EnqueueFirstDeployment(_ context.Context, _ database.Tx, serviceID, _ string) (string, error) {
	f.called = true
	f.serviceID = serviceID
	return testDepID, nil
}

type fakeConfigSetter struct {
	serviceID string
	vars      map[string]string
}

func (f *fakeConfigSetter) SetWithinTx(_ context.Context, _ database.Tx, serviceID string, vars map[string]string) error {
	f.serviceID = serviceID
	f.vars = vars
	return nil
}

type fakeBox struct{}

func (fakeBox) Open(sealed []byte) ([]byte, error) { return sealed, nil }

type fakeGH struct {
	info            github.RepoInfo
	err             error
	files           map[string]string // repo files for DetectFramework (path -> contents)
	installToken    string
	installTokenErr error
}

func (f fakeGH) InstallationToken(_ context.Context, _ string) (string, error) {
	return f.installToken, f.installTokenErr
}

func (f fakeGH) GetRepository(_ context.Context, _, owner, repo string) (github.RepoInfo, error) {
	if f.err != nil {
		return github.RepoInfo{}, f.err
	}
	info := f.info
	if info.Owner == "" {
		info.Owner = owner
		info.Name = repo
		info.FullName = owner + "/" + repo
	}
	return info, nil
}
func (f fakeGH) GetBranch(_ context.Context, _, _, _, _ string) error { return nil }
func (f fakeGH) GetFileContent(_ context.Context, _, _, _, _, path string) ([]byte, bool, error) {
	v, ok := f.files[path]
	if !ok {
		return nil, false, nil
	}
	return []byte(v), true, nil
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

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: "user-1", Method: principal.MethodSession})
}

func newSvc(store Store, gh GitHubClient, enq Enqueuer, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, fakeBox{}, gh, fakeSources{}, enq, &fakeConfigSetter{}, authorizer, rec, slog.Default())
}

// newSvcSrc builds a service with a configured Sources fake, for connected-repo (oauth/app) tests.
func newSvcSrc(store Store, gh GitHubClient, srcs Sources, enq Enqueuer, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, fakeBox{}, gh, srcs, enq, &fakeConfigSetter{}, authorizer, rec, slog.Default())
}

// newDBSvc builds a service with a caller-supplied env setter so CreateDatabase tests can
// inspect the generated credentials it writes.
func newDBSvc(store Store, enq Enqueuer, env ConfigSetter, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, fakeBox{}, fakeGH{}, fakeSources{}, enq, env, authorizer, rec, slog.Default())
}

// fakeSources stubs the Sources port for connected-repo tests.
type fakeSources struct {
	conn    ConnectionMeta
	connOK  bool
	repo    ResolvedRepo
	repoErr error
}

func (f fakeSources) GetConnectionMeta(_ context.Context, _ string) (ConnectionMeta, bool, error) {
	return f.conn, f.connOK, nil
}
func (f fakeSources) ValidateRepo(_ context.Context, _, _, _, _ string) (ResolvedRepo, error) {
	return f.repo, f.repoErr
}

func wantKind(t *testing.T, err error, kind problem.Kind) {
	t.Helper()
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != kind {
		t.Fatalf("got %v, want %v", err, kind)
	}
}

func envResolved() *fakeStore {
	return &fakeStore{envWs: testWorkspace, envProj: testProjectID, envOK: true, serverWs: testWorkspace, serverOK: true}
}

func TestCreateDatabase_PostgresPrivateWithCredsAndDeploy(t *testing.T) {
	store := envResolved()
	rec := &fakeRecorder{}
	enq := &fakeEnqueuer{}
	env := &fakeConfigSetter{}
	svc := newDBSvc(store, enq, env, fakeAuthz{}, rec)

	res, err := svc.CreateDatabase(authedCtx(), DatabaseInput{
		EnvironmentID: testEnvID, Name: "Primary DB", TemplateID: "postgres",
		ServerID: testServerID, DeployNow: true,
	})
	if err != nil {
		t.Fatalf("CreateDatabase failed: %v", err)
	}
	// A database is always a PRIVATE template service built from the catalogue image + port.
	w := store.insertedImage
	if w.SourceKind != SourceTemplate || w.ImageRef != "postgres:16-alpine" || w.ContainerPort != 5432 || w.Visibility != VisibilityPrivate || w.TemplateID != "postgres" {
		t.Errorf("inserted = %+v, want a private postgres template service on :5432", w)
	}
	// The generated credentials are written as THIS service's env vars.
	if env.serviceID != testServiceID {
		t.Errorf("env vars written for %q, want the new service %q", env.serviceID, testServiceID)
	}
	if env.vars["POSTGRES_USER"] != "plorigo" || env.vars["POSTGRES_DB"] != "app" || env.vars["POSTGRES_PASSWORD"] == "" {
		t.Errorf("generated env = %v, want user/db set and a non-empty password", env.vars)
	}
	if !enq.called || res.DeploymentID != testDepID {
		t.Errorf("deploy_now did not enqueue a first deployment (called=%v id=%q)", enq.called, res.DeploymentID)
	}
	if rec.action != "service.create_database" {
		t.Errorf("audit action = %q, want service.create_database", rec.action)
	}
	// The connection URI is consistent with the stored password and the in-network host (slug).
	tmpl, _ := lookupDatabaseTemplate("postgres")
	if want := tmpl.connectionURI(res.Service.Slug, "plorigo", env.vars["POSTGRES_PASSWORD"], "app"); res.ConnectionURI != want {
		t.Errorf("connection uri = %q, want %q", res.ConnectionURI, want)
	}
}

func TestCreateDatabase_AppliesCallerOptions(t *testing.T) {
	store := envResolved()
	env := &fakeConfigSetter{}
	svc := newDBSvc(store, &fakeEnqueuer{}, env, fakeAuthz{}, &fakeRecorder{})

	const pw = "Sup3r-Secret_pw"
	res, err := svc.CreateDatabase(authedCtx(), DatabaseInput{
		EnvironmentID: testEnvID, Name: "Primary DB", TemplateID: "postgres",
		DatabaseName: "orders", Username: "app_user", Password: pw,
	})
	if err != nil {
		t.Fatalf("CreateDatabase failed: %v", err)
	}
	// The caller's database name, user, and password override the template defaults.
	if env.vars["POSTGRES_USER"] != "app_user" || env.vars["POSTGRES_DB"] != "orders" || env.vars["POSTGRES_PASSWORD"] != pw {
		t.Errorf("env = %v, want user=app_user db=orders and the supplied password", env.vars)
	}
	tmpl, _ := lookupDatabaseTemplate("postgres")
	if want := tmpl.connectionURI(res.Service.Slug, "app_user", pw, "orders"); res.ConnectionURI != want {
		t.Errorf("connection uri = %q, want %q", res.ConnectionURI, want)
	}
}

func TestCreateDatabase_BlankOptionsFallBackToDefaults(t *testing.T) {
	store := envResolved()
	env := &fakeConfigSetter{}
	svc := newDBSvc(store, &fakeEnqueuer{}, env, fakeAuthz{}, &fakeRecorder{})

	_, err := svc.CreateDatabase(authedCtx(), DatabaseInput{
		EnvironmentID: testEnvID, Name: "db", TemplateID: "postgres",
		DatabaseName: "  ", Username: "", Password: "",
	})
	if err != nil {
		t.Fatalf("CreateDatabase failed: %v", err)
	}
	if env.vars["POSTGRES_USER"] != "plorigo" || env.vars["POSTGRES_DB"] != "app" || env.vars["POSTGRES_PASSWORD"] == "" {
		t.Errorf("env = %v, want template defaults and a generated password", env.vars)
	}
}

func TestCreateDatabase_RejectsInvalidOptions(t *testing.T) {
	cases := map[string]DatabaseInput{
		"bad database name": {EnvironmentID: testEnvID, Name: "db", TemplateID: "postgres", DatabaseName: "no spaces"},
		"bad username":      {EnvironmentID: testEnvID, Name: "db", TemplateID: "postgres", Username: "1starts-with-digit"},
		"short password":    {EnvironmentID: testEnvID, Name: "db", TemplateID: "postgres", Password: "short"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			store := envResolved()
			env := &fakeConfigSetter{}
			svc := newDBSvc(store, &fakeEnqueuer{}, env, fakeAuthz{}, &fakeRecorder{})
			_, err := svc.CreateDatabase(authedCtx(), in)
			wantKind(t, err, problem.KindInvalidInput)
			if store.insertedImage.Name != "" || env.serviceID != "" {
				t.Error("invalid options wrote a service or config")
			}
		})
	}
}

func TestCreateDatabase_WithoutDeployStillProvisions(t *testing.T) {
	store := envResolved()
	enq := &fakeEnqueuer{}
	env := &fakeConfigSetter{}
	svc := newDBSvc(store, enq, env, fakeAuthz{}, &fakeRecorder{})

	res, err := svc.CreateDatabase(authedCtx(), DatabaseInput{EnvironmentID: testEnvID, Name: "db", TemplateID: "postgres"})
	if err != nil {
		t.Fatalf("CreateDatabase failed: %v", err)
	}
	if enq.called || res.DeploymentID != "" {
		t.Errorf("enqueued a deployment without deploy_now (called=%v id=%q)", enq.called, res.DeploymentID)
	}
	if env.vars["POSTGRES_PASSWORD"] == "" || res.ConnectionURI == "" {
		t.Error("provisioning should still generate credentials and a connection URI")
	}
}

func TestCreateDatabase_UnknownTemplateRejected(t *testing.T) {
	svc := newDBSvc(envResolved(), &fakeEnqueuer{}, &fakeConfigSetter{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateDatabase(authedCtx(), DatabaseInput{EnvironmentID: testEnvID, Name: "db", TemplateID: "mysql"})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreateDatabase_DeniedWritesNothing(t *testing.T) {
	store := envResolved()
	env := &fakeConfigSetter{}
	rec := &fakeRecorder{}
	svc := newDBSvc(store, &fakeEnqueuer{}, env, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)

	_, err := svc.CreateDatabase(authedCtx(), DatabaseInput{EnvironmentID: testEnvID, Name: "db", TemplateID: "postgres", ServerID: testServerID, DeployNow: true})
	wantKind(t, err, problem.KindPermissionDenied)
	if store.insertedImage.Name != "" || env.serviceID != "" || rec.called {
		t.Error("denied CreateDatabase wrote a service, env vars, or audit row")
	}
}

func TestCreateService_ImageDeployNow(t *testing.T) {
	store := envResolved()
	rec := &fakeRecorder{}
	enq := &fakeEnqueuer{}
	svc := newSvc(store, fakeGH{}, enq, fakeAuthz{}, rec)

	res, err := svc.CreateService(authedCtx(), CreateInput{
		EnvironmentID: testEnvID, Name: "Web API", SourceKind: SourceImage, ImageRef: "nginx",
		ContainerPort: 80, ServerID: testServerID, DeployNow: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedImage.Slug != "web-api" || store.insertedImage.ImageRef != "nginx:latest" {
		t.Errorf("inserted = %+v, want slugified name + :latest image", store.insertedImage)
	}
	if store.insertedImage.Visibility != VisibilityPublic {
		t.Errorf("visibility = %q, want default public", store.insertedImage.Visibility)
	}
	if !enq.called || enq.serviceID != testServiceID || res.DeploymentID != testDepID {
		t.Errorf("enqueue = (%v,%q) dep=%q, want first deployment enqueued", enq.called, enq.serviceID, res.DeploymentID)
	}
	if !rec.called || rec.action != "service.create" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCreateService_GitPublicDeployNow(t *testing.T) {
	store := envResolved()
	enq := &fakeEnqueuer{}
	svc := newSvc(store, fakeGH{info: github.RepoInfo{DefaultBranch: "main"}}, enq, fakeAuthz{}, &fakeRecorder{})

	res, err := svc.CreateService(authedCtx(), CreateInput{
		EnvironmentID: testEnvID, Name: "web", SourceKind: SourceGit,
		RepoURL: "https://github.com/o/r", ContainerPort: 0, ServerID: testServerID, DeployNow: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedGit.SourceAccess != accessPublic || store.insertedGit.ConnectionID != "" {
		t.Errorf("inserted = %+v, want public access + no connection", store.insertedGit)
	}
	if store.insertedGit.Branch != "main" {
		t.Errorf("branch = %q, want default branch", store.insertedGit.Branch)
	}
	if res.DeploymentID != testDepID {
		t.Error("a public git service with deploy_now should enqueue a build")
	}
}

func TestCreateService_GitOAuthDeployNowSkipsDeploy(t *testing.T) {
	store := envResolved()
	enq := &fakeEnqueuer{}
	srcs := fakeSources{
		conn:   ConnectionMeta{WorkspaceID: testWorkspace, Provider: "github", Kind: "oauth", AccountLogin: "octocat"},
		connOK: true,
		repo:   ResolvedRepo{Owner: "o", Name: "r", FullName: "o/r", DefaultBranch: "main", Branch: "main", IsPrivate: true, Kind: "oauth", AccountLogin: "octocat", Buildable: false},
	}
	svc := newSvcSrc(store, fakeGH{}, srcs, enq, fakeAuthz{}, &fakeRecorder{})

	res, err := svc.CreateService(authedCtx(), CreateInput{
		EnvironmentID: testEnvID, Name: "api", SourceKind: SourceGit,
		ConnectionID: "conn-1", Owner: "o", Repo: "r", ContainerPort: 0, ServerID: testServerID, DeployNow: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedGit.SourceAccess != accessOAuth || store.insertedGit.ConnectionID != "conn-1" {
		t.Errorf("inserted = %+v, want oauth access + connection id", store.insertedGit)
	}
	// An OAuth repo isn't buildable: the service is created but no deployment is enqueued.
	if enq.called || res.DeploymentID != "" {
		t.Errorf("enqueue=%v dep=%q, want NO deployment for an oauth git service", enq.called, res.DeploymentID)
	}
	if res.Service.GitHubLogin != "octocat" {
		t.Errorf("github_login = %q, want the connected account", res.Service.GitHubLogin)
	}
}

func TestCreateService_DeniedWritesNothing(t *testing.T) {
	store := envResolved()
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeGH{}, &fakeEnqueuer{}, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	_, err := svc.CreateService(authedCtx(), CreateInput{EnvironmentID: testEnvID, Name: "web", SourceKind: SourceImage, ImageRef: "nginx", ContainerPort: 80})
	wantKind(t, err, problem.KindPermissionDenied)
	if store.insertedImage.Name != "" || rec.called {
		t.Error("a denied create must not insert or audit")
	}
}

func TestCreateService_InvalidName(t *testing.T) {
	svc := newSvc(envResolved(), fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateService(authedCtx(), CreateInput{EnvironmentID: testEnvID, Name: "  ", SourceKind: SourceImage, ImageRef: "nginx", ContainerPort: 80})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreateService_ImageRequiresPort(t *testing.T) {
	svc := newSvc(envResolved(), fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateService(authedCtx(), CreateInput{EnvironmentID: testEnvID, Name: "web", SourceKind: SourceImage, ImageRef: "nginx", ContainerPort: 0})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreateService_EnvironmentNotFound(t *testing.T) {
	store := &fakeStore{envOK: false}
	svc := newSvc(store, fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateService(authedCtx(), CreateInput{EnvironmentID: testEnvID, Name: "web", SourceKind: SourceImage, ImageRef: "nginx", ContainerPort: 80})
	wantKind(t, err, problem.KindNotFound)
}

func TestCreateService_GitAppDeployNowBuildsPrivate(t *testing.T) {
	store := envResolved()
	enq := &fakeEnqueuer{}
	// An App-backed connection: validated through the sources seam, buildable, deployed now.
	srcs := fakeSources{
		conn:   ConnectionMeta{WorkspaceID: testWorkspace, Provider: "github", Kind: "app"},
		connOK: true,
		repo:   ResolvedRepo{Owner: "o", Name: "r", FullName: "o/r", DefaultBranch: "main", Branch: "main", IsPrivate: true, Kind: "app", Buildable: true},
	}
	svc := newSvcSrc(store, fakeGH{}, srcs, enq, fakeAuthz{}, &fakeRecorder{})

	res, err := svc.CreateService(authedCtx(), CreateInput{
		EnvironmentID: testEnvID, Name: "api", SourceKind: SourceGit,
		ConnectionID: "appconn-1", Owner: "o", Repo: "r", ContainerPort: 0, ServerID: testServerID, DeployNow: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// App-backed source: buildable, linked to the App connection, and deployed now (unlike OAuth).
	if store.insertedGit.SourceAccess != accessApp || store.insertedGit.ConnectionID != "appconn-1" {
		t.Errorf("inserted = %+v, want app access + app connection id", store.insertedGit)
	}
	if !enq.called || res.DeploymentID == "" {
		t.Errorf("enqueue=%v dep=%q, want a deployment enqueued for an app git service", enq.called, res.DeploymentID)
	}
}

func TestCreateService_PublicGitRejectsPrivateRepo(t *testing.T) {
	store := envResolved()
	svc := newSvc(store, fakeGH{info: github.RepoInfo{Owner: "o", Name: "r", FullName: "o/r", Private: true}}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateService(authedCtx(), CreateInput{EnvironmentID: testEnvID, Name: "web", SourceKind: SourceGit, RepoURL: "https://github.com/o/r", ServerID: testServerID, DeployNow: true})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreateService_DeployNowServerOtherWorkspace(t *testing.T) {
	store := envResolved()
	store.serverWs = "other-ws"
	svc := newSvc(store, fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateService(authedCtx(), CreateInput{EnvironmentID: testEnvID, Name: "web", SourceKind: SourceImage, ImageRef: "nginx", ContainerPort: 80, ServerID: testServerID, DeployNow: true})
	wantKind(t, err, problem.KindNotFound)
}

func TestGetService_AuthorizesAndReturns(t *testing.T) {
	store := &fakeStore{getOK: true, getSvc: Service{ID: testServiceID, WorkspaceID: testWorkspace, Name: "web"}}
	svc := newSvc(store, fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	got, err := svc.GetService(authedCtx(), testServiceID)
	if err != nil || got.ID != testServiceID {
		t.Fatalf("got (%+v, %v), want the service", got, err)
	}
}

func TestGetService_NotFound(t *testing.T) {
	svc := newSvc(&fakeStore{getOK: false}, fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.GetService(authedCtx(), testServiceID)
	wantKind(t, err, problem.KindNotFound)
}

func TestUpdateVisibility_AuditsAndReturns(t *testing.T) {
	store := &fakeStore{svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, rec)
	got, err := svc.UpdateVisibility(authedCtx(), testServiceID, VisibilityPrivate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Visibility != VisibilityPrivate || !rec.called || rec.action != "service.update" {
		t.Errorf("got %+v audit=(%v,%q), want private + audited", got, rec.called, rec.action)
	}
}

func TestDeleteService_NotFound(t *testing.T) {
	store := &fakeStore{svcWs: testWorkspace, svcMetaOK: true, getOK: false}
	svc := newSvc(store, fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	err := svc.DeleteService(authedCtx(), testServiceID)
	wantKind(t, err, problem.KindNotFound)
}

func TestDetectFramework_Detected(t *testing.T) {
	gh := fakeGH{
		info: github.RepoInfo{DefaultBranch: "main"},
		files: map[string]string{
			"package.json":   `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14"}}`,
			"pnpm-lock.yaml": "lockfileVersion: '9.0'",
		},
	}
	svc := newSvc(envResolved(), gh, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	got, err := svc.DetectFramework(authedCtx(), DetectInput{RepoURL: "https://github.com/o/r"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != "detected" || got.Runtime != "nextjs" || got.RuntimeLabel != "Next.js" {
		t.Errorf("got %+v, want a detected Next.js plan", got)
	}
	if got.PackageManager != "pnpm" || got.ContainerPort != 3000 || got.Dockerfile == "" {
		t.Errorf("got %+v, want pnpm + port 3000 + a rendered Dockerfile", got)
	}
}

func TestDetectFramework_Unsupported(t *testing.T) {
	gh := fakeGH{info: github.RepoInfo{DefaultBranch: "main"}, files: map[string]string{"README.md": "hi"}}
	svc := newSvc(envResolved(), gh, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	got, err := svc.DetectFramework(authedCtx(), DetectInput{RepoURL: "o/r"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != "unsupported" || got.NextSteps == "" {
		t.Errorf("got %+v, want unsupported with next steps", got)
	}
}

func TestDetectFramework_PrivateRejected(t *testing.T) {
	gh := fakeGH{info: github.RepoInfo{Owner: "o", Name: "r", FullName: "o/r", Private: true}}
	svc := newSvc(envResolved(), gh, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.DetectFramework(authedCtx(), DetectInput{RepoURL: "https://github.com/o/r"})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestDetectFramework_BadURL(t *testing.T) {
	svc := newSvc(envResolved(), fakeGH{}, &fakeEnqueuer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.DetectFramework(authedCtx(), DetectInput{RepoURL: ""})
	wantKind(t, err, problem.KindInvalidInput)
}
