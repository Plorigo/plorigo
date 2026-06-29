package deployments

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testServiceID = "00000000-0000-0000-0000-0000000000aa"
	testEnvID     = "11111111-1111-1111-1111-111111111111"
	testServerID  = "22222222-2222-2222-2222-222222222222"
	testProjectID = "33333333-3333-3333-3333-333333333333"
	testDeployID  = "44444444-4444-4444-4444-444444444444"
	testAgentID   = "55555555-5555-5555-5555-555555555555"
	otherServerID = "66666666-6666-6666-6666-666666666666"
	testWorkspace = "ws-1"

	testTeardownID = "77777777-7777-7777-7777-777777777777"
)

type fakeStore struct {
	// WorkspaceAndProjectForService
	svcWs, svcProj string
	svcMetaOK      bool

	// ServiceForDeploy / ServiceForDeployTx
	svc   ServiceForDeploy
	svcOK bool

	envWs, envProj string
	envOK          bool
	envErr         error

	serverWs string
	serverOK bool

	projWs string
	projOK bool

	credAgentID, credServerID string
	credOK                    bool

	config []ConfigForDeploy

	inserted        NewDeployment
	insertedGit     NewDeploymentFromGit
	insertedPreview NewPreviewDeployment
	insertErr       error

	getDep Deployment
	getOK  bool

	claimDep Deployment
	claimOK  bool

	statusUpdates    []StatusUpdate
	events           []NewEvent
	supersededWith   string // route_key passed to SupersedePreviousRunning
	superseded       bool
	routeServiceID   string
	routeURLReported string
	verifiedDomains  map[string][]string
	routeSyncStatus  string
	routeSyncMessage string
	routeSyncHosts   []string

	// Teardown jobs.
	insertedTeardown      NewTeardownJob
	getTeardown           TeardownJob
	getTeardownOK         bool
	claimTeardown         TeardownJob
	claimTeardownOK       bool
	teardownStatusUpdates []TeardownStatusUpdate
	tornDownRouteKey      string
	tornDownServerID      string

	// Webhook seam.
	latestServerID  string
	latestServerOK  bool
	activePreview   Deployment
	activePreviewOK bool
}

func (f *fakeStore) WorkspaceAndProjectForEnvironment(_ context.Context, _ string) (string, string, bool, error) {
	return f.envWs, f.envProj, f.envOK, f.envErr
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
func (f *fakeStore) AgentServerByCredential(_ context.Context, _ []byte) (string, string, bool, error) {
	return f.credAgentID, f.credServerID, f.credOK, nil
}
func (f *fakeStore) ConfigForService(_ context.Context, _ string) ([]ConfigForDeploy, error) {
	return f.config, nil
}
func (f *fakeStore) ServiceForDeploy(_ context.Context, _ string) (ServiceForDeploy, bool, error) {
	return f.svc, f.svcOK, nil
}
func (f *fakeStore) ServiceForDeployTx(_ context.Context, _ database.Tx, _ string) (ServiceForDeploy, bool, error) {
	return f.svc, f.svcOK, nil
}
func (f *fakeStore) InsertDeployment(_ context.Context, _ database.Tx, d NewDeployment) (Deployment, error) {
	f.inserted = d
	if f.insertErr != nil {
		return Deployment{}, f.insertErr
	}
	return Deployment{
		ID:            testDeployID,
		ServiceID:     d.ServiceID,
		EnvironmentID: d.EnvironmentID,
		ProjectID:     d.ProjectID,
		WorkspaceID:   d.WorkspaceID,
		ServerID:      d.ServerID,
		ImageRef:      d.ImageRef,
		ContainerPort: d.ContainerPort,
		Status:        StatusQueued,
	}, nil
}
func (f *fakeStore) InsertDeploymentFromGit(_ context.Context, _ database.Tx, d NewDeploymentFromGit) (Deployment, error) {
	f.insertedGit = d
	if f.insertErr != nil {
		return Deployment{}, f.insertErr
	}
	return Deployment{
		ID:            testDeployID,
		ServiceID:     d.ServiceID,
		EnvironmentID: d.EnvironmentID,
		ProjectID:     d.ProjectID,
		WorkspaceID:   d.WorkspaceID,
		ServerID:      d.ServerID,
		ContainerPort: d.ContainerPort,
		Status:        StatusQueued,
		SourceKind:    SourceGit,
		SourceAccess:  d.SourceAccess,
		CloneURL:      d.CloneURL,
		GitRef:        d.GitRef,
	}, nil
}
func (f *fakeStore) InsertPreviewDeployment(_ context.Context, _ database.Tx, d NewPreviewDeployment) (Deployment, error) {
	f.insertedPreview = d
	if f.insertErr != nil {
		return Deployment{}, f.insertErr
	}
	return Deployment{
		ID:            testDeployID,
		ServiceID:     d.ServiceID,
		RouteKey:      d.RouteKey,
		Kind:          KindPreview,
		EnvironmentID: d.EnvironmentID,
		ProjectID:     d.ProjectID,
		WorkspaceID:   d.WorkspaceID,
		ServerID:      d.ServerID,
		ContainerPort: d.ContainerPort,
		Status:        StatusQueued,
		SourceKind:    SourceGit,
		SourceAccess:  d.SourceAccess,
		CloneURL:      d.CloneURL,
		GitRef:        d.GitRef,
		PRNumber:      d.PRNumber,
		PRURL:         d.PRURL,
	}, nil
}
func (f *fakeStore) GetDeployment(_ context.Context, _ string) (Deployment, bool, error) {
	return f.getDep, f.getOK, nil
}
func (f *fakeStore) ListByService(_ context.Context, _ string) ([]Deployment, error) { return nil, nil }
func (f *fakeStore) ListByEnvironment(_ context.Context, _ string) ([]Deployment, error) {
	return nil, nil
}
func (f *fakeStore) ListByProject(_ context.Context, _ string) ([]Deployment, error) { return nil, nil }
func (f *fakeStore) ListByWorkspace(_ context.Context, _ string) ([]Deployment, error) {
	return nil, nil
}
func (f *fakeStore) ListEvents(_ context.Context, _ string, _ int64) ([]Event, error) {
	return nil, nil
}
func (f *fakeStore) ClaimNextForServer(_ context.Context, _ database.Tx, _ string) (Deployment, bool, error) {
	return f.claimDep, f.claimOK, nil
}
func (f *fakeStore) UpdateStatus(_ context.Context, _ database.Tx, u StatusUpdate) error {
	f.statusUpdates = append(f.statusUpdates, u)
	return nil
}
func (f *fakeStore) SupersedePreviousRunning(_ context.Context, _ database.Tx, routeKey, _, _ string) error {
	f.superseded = true
	f.supersededWith = routeKey
	return nil
}
func (f *fakeStore) UpdateServiceRouteURL(_ context.Context, _ database.Tx, serviceID, routeURL string) error {
	f.routeServiceID = serviceID
	f.routeURLReported = routeURL
	return nil
}
func (f *fakeStore) AppendEvent(_ context.Context, _ database.Tx, e NewEvent) error {
	f.events = append(f.events, e)
	return nil
}
func (f *fakeStore) VerifiedDomainsForServices(_ context.Context, serviceIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, serviceID := range serviceIDs {
		out[serviceID] = f.verifiedDomains[serviceID]
	}
	return out, nil
}
func (f *fakeStore) MarkDomainsRouteSync(_ context.Context, _ database.Tx, _ string, hostnames []string, status, message string) error {
	f.routeSyncHosts = append([]string(nil), hostnames...)
	f.routeSyncStatus = status
	f.routeSyncMessage = message
	return nil
}
func (f *fakeStore) InsertTeardownJob(_ context.Context, _ database.Tx, t NewTeardownJob) (TeardownJob, error) {
	f.insertedTeardown = t
	if f.insertErr != nil {
		return TeardownJob{}, f.insertErr
	}
	return TeardownJob{
		ID:            testTeardownID,
		DeploymentID:  t.DeploymentID,
		ServiceID:     t.ServiceID,
		RouteKey:      t.RouteKey,
		EnvironmentID: t.EnvironmentID,
		ProjectID:     t.ProjectID,
		WorkspaceID:   t.WorkspaceID,
		ServerID:      t.ServerID,
		Status:        TeardownStatusQueued,
	}, nil
}
func (f *fakeStore) GetTeardownJob(_ context.Context, _ string) (TeardownJob, bool, error) {
	return f.getTeardown, f.getTeardownOK, nil
}
func (f *fakeStore) ListTeardownsByService(_ context.Context, _ string) ([]TeardownJob, error) {
	return nil, nil
}
func (f *fakeStore) ClaimNextTeardownForServer(_ context.Context, _ database.Tx, _ string) (TeardownJob, bool, error) {
	return f.claimTeardown, f.claimTeardownOK, nil
}
func (f *fakeStore) UpdateTeardownStatus(_ context.Context, _ database.Tx, u TeardownStatusUpdate) (TeardownJob, error) {
	f.teardownStatusUpdates = append(f.teardownStatusUpdates, u)
	return TeardownJob{ID: u.TeardownID, Status: u.Status}, nil
}
func (f *fakeStore) MarkPreviewTornDown(_ context.Context, _ database.Tx, routeKey, serverID string) error {
	f.tornDownRouteKey = routeKey
	f.tornDownServerID = serverID
	return nil
}
func (f *fakeStore) LatestServerForService(_ context.Context, _ string) (string, bool, error) {
	return f.latestServerID, f.latestServerOK, nil
}
func (f *fakeStore) LatestActivePreviewByRouteKey(_ context.Context, _, _ string) (Deployment, bool, error) {
	return f.activePreview, f.activePreviewOK, nil
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

func newSvc(store Store, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, authorizer, rec, fakeOpener{}, fakeGitHub{}, slog.Default())
}

// newSvcGH builds the service with a specific GitHub fake + recorder (preview tests).
func newSvcGH(store Store, gh GitHubClient, rec Recorder) *service {
	return newService(fakeTx{}, store, fakeAuthz{}, rec, fakeOpener{}, gh, slog.Default())
}

// fakeGitHub stubs the GitHubClient port; pr/prErr drive GetPullRequest.
type fakeGitHub struct {
	pr    github.PullRequest
	prErr error
}

func (f fakeGitHub) GetPullRequest(_ context.Context, _, _, _ string, _ int) (github.PullRequest, error) {
	return f.pr, f.prErr
}

// fakeOpener returns the sealed bytes with a "sealed:" prefix stripped, mirroring fakeSealer
// in the config module, so a deploy-time secret decrypts to a recognizable plaintext.
type fakeOpener struct{}

func (fakeOpener) Open(sealed []byte) ([]byte, error) {
	return []byte(strings.TrimPrefix(string(sealed), "sealed:")), nil
}

func strptr(s string) *string { return &s }

func wantKind(t *testing.T, err error, kind problem.Kind) {
	t.Helper()
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != kind {
		t.Fatalf("got %v, want %v", err, kind)
	}
}

// imageService is a resolved image service used across CreateForService tests.
func imageService() ServiceForDeploy {
	return ServiceForDeploy{
		EnvironmentID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace,
		SourceKind: SourceImage, ImageRef: "traefik/whoami", ContainerPort: 80,
		Visibility: "public", Slug: "web",
	}
}

func TestCreateForService_AuthorizedInsertsQueuedAndAudits(t *testing.T) {
	store := &fakeStore{
		svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true,
		svc: imageService(), svcOK: true,
		serverWs: testWorkspace, serverOK: true,
	}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	dep, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dep.Status != StatusQueued {
		t.Errorf("status = %q, want %q", dep.Status, StatusQueued)
	}
	if store.inserted.ImageRef != "traefik/whoami:latest" {
		t.Errorf("image = %q, want :latest defaulted", store.inserted.ImageRef)
	}
	if store.inserted.ServiceID != testServiceID || store.inserted.ProjectID != testProjectID || store.inserted.WorkspaceID != testWorkspace {
		t.Errorf("inserted = %+v, want denormalized service/project/workspace", store.inserted)
	}
	if !rec.called || rec.action != "deployment.create" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCreateForService_PortOverride(t *testing.T) {
	store := &fakeStore{
		svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true,
		svc: imageService(), svcOK: true, serverWs: testWorkspace, serverOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID, ContainerPort: 9090}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.inserted.ContainerPort != 9090 {
		t.Errorf("port = %d, want the override 9090", store.inserted.ContainerPort)
	}
}

func TestCreateForService_DeniedWritesNothing(t *testing.T) {
	store := &fakeStore{svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true, svc: imageService(), svcOK: true}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)

	_, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID})
	wantKind(t, err, problem.KindPermissionDenied)
	if store.inserted.ImageRef != "" || rec.called {
		t.Error("a denied create must not insert or audit")
	}
}

func TestCreateForService_InvalidImageRef(t *testing.T) {
	bad := imageService()
	bad.ImageRef = "  "
	store := &fakeStore{svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true, svc: bad, svcOK: true, serverWs: testWorkspace, serverOK: true}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreateForService_InvalidPort(t *testing.T) {
	noPort := imageService()
	noPort.ContainerPort = 0
	store := &fakeStore{svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true, svc: noPort, svcOK: true, serverWs: testWorkspace, serverOK: true}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreateForService_ServiceNotFound(t *testing.T) {
	store := &fakeStore{svcMetaOK: false}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID})
	wantKind(t, err, problem.KindNotFound)
}

func TestCreateForService_ServerInOtherWorkspaceNotFound(t *testing.T) {
	store := &fakeStore{svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true, svc: imageService(), svcOK: true, serverWs: "other-ws", serverOK: true}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID})
	wantKind(t, err, problem.KindNotFound)
	if store.inserted.ImageRef != "" {
		t.Error("a cross-workspace server must not insert a deployment")
	}
}

func TestCreateForService_GitPublicInsertsGitDeployment(t *testing.T) {
	store := &fakeStore{
		svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true,
		svc: ServiceForDeploy{
			EnvironmentID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace,
			SourceKind: SourceGit, SourceAccess: "public", Owner: "o", Repo: "r",
			Branch: "feature", DefaultBranch: "main", Slug: "web",
		},
		svcOK: true, serverWs: testWorkspace, serverOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	dep, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dep.SourceKind != SourceGit {
		t.Errorf("source kind = %q, want git", dep.SourceKind)
	}
	if store.insertedGit.CloneURL != "https://github.com/o/r.git" || store.insertedGit.SourceAccess != "public" {
		t.Errorf("inserted = %+v, want public clone url", store.insertedGit)
	}
	if store.insertedGit.GitRef != "feature" { // defaults to the service's branch
		t.Errorf("git_ref = %q, want the service branch defaulted", store.insertedGit.GitRef)
	}
}

func TestCreateForService_GitRefOverrideAndDefaultBranch(t *testing.T) {
	store := &fakeStore{
		svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true,
		svc: ServiceForDeploy{
			EnvironmentID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace,
			SourceKind: SourceGit, SourceAccess: "public", Owner: "o", Repo: "r", DefaultBranch: "main",
		},
		svcOK: true, serverWs: testWorkspace, serverOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	// No explicit ref and no branch -> default branch.
	if _, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedGit.GitRef != "main" {
		t.Errorf("git_ref = %q, want default branch", store.insertedGit.GitRef)
	}
	// Explicit override wins.
	if _, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID, GitRef: "v2"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedGit.GitRef != "v2" {
		t.Errorf("git_ref = %q, want the override", store.insertedGit.GitRef)
	}
}

func TestCreateForService_RejectsPrivateGit(t *testing.T) {
	store := &fakeStore{
		svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true,
		svc: ServiceForDeploy{
			EnvironmentID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace,
			SourceKind: SourceGit, SourceAccess: "oauth", Owner: "o", Repo: "r", Branch: "main",
		},
		svcOK: true, serverWs: testWorkspace, serverOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateForService(authedCtx(), CreateForServiceInput{ServiceID: testServiceID, ServerID: testServerID})
	wantKind(t, err, problem.KindInvalidInput)
	if store.insertedGit.CloneURL != "" {
		t.Error("a private git source must not insert a deployment (public-first)")
	}
}

// gitPublicService is a resolved public git service used across CreatePreview tests.
func gitPublicService() ServiceForDeploy {
	return ServiceForDeploy{
		EnvironmentID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace,
		SourceKind: SourceGit, SourceAccess: "public", Owner: "o", Repo: "r",
		Branch: "main", DefaultBranch: "main", ContainerPort: 3000, Slug: "web",
	}
}

func previewStore() *fakeStore {
	return &fakeStore{
		svcWs: testWorkspace, svcProj: testProjectID, svcMetaOK: true,
		svc: gitPublicService(), svcOK: true, serverWs: testWorkspace, serverOK: true,
	}
}

func TestCreatePreview_BranchInsertsPreviewWithRouteKey(t *testing.T) {
	store := previewStore()
	rec := &fakeRecorder{}
	svc := newSvcGH(store, fakeGitHub{}, rec)

	dep, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID, Branch: "feature/x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dep.Kind != KindPreview {
		t.Errorf("kind = %q, want preview", dep.Kind)
	}
	p := store.insertedPreview
	if p.RouteKey != testServiceID+"-feature-x" {
		t.Errorf("route_key = %q, want service id + slugified branch", p.RouteKey)
	}
	if p.GitRef != "feature/x" || p.PRNumber != 0 || p.PRURL != "" {
		t.Errorf("inserted = %+v, want the branch ref and no PR linkage", p)
	}
	if p.CloneURL != "https://github.com/o/r.git" || p.SourceAccess != "public" {
		t.Errorf("inserted = %+v, want the service's public clone url", p)
	}
	if !rec.called || rec.action != "deployment.preview" {
		t.Errorf("audit = (%v, %q), want deployment.preview", rec.called, rec.action)
	}
}

func TestCreatePreview_PRResolvesHeadRefAndLinks(t *testing.T) {
	store := previewStore()
	gh := fakeGitHub{pr: github.PullRequest{Number: 7, State: "open", HeadRef: "contributor:feat", HTMLURL: "https://github.com/o/r/pull/7"}}
	svc := newSvcGH(store, gh, &fakeRecorder{})

	if _, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID, PRNumber: 7}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := store.insertedPreview
	// The PR's head ref is what gets built; the route_key is keyed by PR number (stable across
	// pushes); the PR is linked back via its URL.
	if p.GitRef != "contributor:feat" {
		t.Errorf("git_ref = %q, want the PR head ref", p.GitRef)
	}
	if p.RouteKey != testServiceID+"-pr-7" {
		t.Errorf("route_key = %q, want service id + pr-7", p.RouteKey)
	}
	if p.PRNumber != 7 || p.PRURL != "https://github.com/o/r/pull/7" {
		t.Errorf("inserted = %+v, want PR number + URL linked", p)
	}
}

func TestCreatePreview_RequiresExactlyOneOfBranchOrPR(t *testing.T) {
	svc := newSvcGH(previewStore(), fakeGitHub{}, &fakeRecorder{})
	// Neither.
	_, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID})
	wantKind(t, err, problem.KindInvalidInput)
	// Both.
	_, err = svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID, Branch: "x", PRNumber: 3})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreatePreview_RejectsNonPublicGit(t *testing.T) {
	store := previewStore()
	oauth := gitPublicService()
	oauth.SourceAccess = "oauth"
	store.svc = oauth
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})
	_, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID, Branch: "x"})
	wantKind(t, err, problem.KindInvalidInput)
	if store.insertedPreview.RouteKey != "" {
		t.Error("a non-public git service must not insert a preview")
	}
}

func TestCreatePreview_RejectsNonGitService(t *testing.T) {
	store := previewStore()
	store.svc = imageService()
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})
	_, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID, Branch: "x"})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreatePreview_PRNotFoundSurfaces(t *testing.T) {
	store := previewStore()
	svc := newSvcGH(store, fakeGitHub{prErr: github.ErrNotFound}, &fakeRecorder{})
	_, err := svc.CreatePreview(authedCtx(), CreatePreviewInput{ServiceID: testServiceID, ServerID: testServerID, PRNumber: 99})
	wantKind(t, err, problem.KindNotFound)
	if store.insertedPreview.RouteKey != "" {
		t.Error("a failed PR lookup must not insert a preview")
	}
}

func TestPreviewRouteKey_TruncatesLongBranchWithHash(t *testing.T) {
	long := strings.Repeat("very-long-branch-name", 5)
	key := previewRouteKey(testServiceID, 0, long)
	if len(key) > maxRouteKeyLen {
		t.Errorf("route_key length = %d, want <= %d", len(key), maxRouteKeyLen)
	}
	// Distinct long refs must produce distinct keys (hash tail).
	if other := previewRouteKey(testServiceID, 0, long+"-x"); other == key {
		t.Error("distinct long refs collided on the same route_key")
	}
}

func TestRollbackToDeployment_ImageReproducesArtifactAndLinks(t *testing.T) {
	store := &fakeStore{
		getOK: true,
		getDep: Deployment{
			ID: testDeployID, ServiceID: testServiceID, EnvironmentID: testEnvID,
			ProjectID: testProjectID, WorkspaceID: testWorkspace, ServerID: testServerID,
			ImageRef: "traefik/whoami:latest", ContainerPort: 80,
			Status: StatusSuperseded, SourceKind: SourceImage,
		},
	}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	if _, err := svc.RollbackToDeployment(authedCtx(), testDeployID); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if store.inserted.RolledBackFrom != testDeployID {
		t.Errorf("rolled_back_from = %q, want target id %q", store.inserted.RolledBackFrom, testDeployID)
	}
	if store.inserted.ImageRef != "traefik/whoami:latest" || store.inserted.ContainerPort != 80 {
		t.Errorf("inserted = %+v, want the target's image + port reproduced", store.inserted)
	}
	if store.inserted.ServiceID != testServiceID || store.inserted.ServerID != testServerID {
		t.Errorf("inserted scope = %+v, want the same service + server", store.inserted)
	}
	if rec.action != "deployment.rollback" {
		t.Errorf("audit action = %q, want deployment.rollback", rec.action)
	}
}

func TestRollbackToDeployment_GitPinsToBuiltCommit(t *testing.T) {
	store := &fakeStore{
		getOK: true,
		getDep: Deployment{
			ID: testDeployID, ServiceID: testServiceID, EnvironmentID: testEnvID,
			ProjectID: testProjectID, WorkspaceID: testWorkspace, ServerID: testServerID,
			ContainerPort: 3000, Status: StatusRunning,
			SourceKind: SourceGit, SourceAccess: "public",
			CloneURL: "https://github.com/o/r.git", GitRef: "main", CommitSha: "deadbeefcafe",
		},
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})

	if _, err := svc.RollbackToDeployment(authedCtx(), testDeployID); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	g := store.insertedGit
	if g.RolledBackFrom != testDeployID {
		t.Errorf("rolled_back_from = %q, want %q", g.RolledBackFrom, testDeployID)
	}
	// Pin to the exact commit the target built, not its branch, so the rebuild is reproducible.
	if g.GitRef != "deadbeefcafe" {
		t.Errorf("git ref = %q, want the built commit", g.GitRef)
	}
	if g.CloneURL != "https://github.com/o/r.git" || g.SourceAccess != "public" || g.ContainerPort != 3000 {
		t.Errorf("inserted git = %+v, want the target's repo/access/port reproduced", g)
	}
}

func TestRollbackToDeployment_RejectsUnhealthyTarget(t *testing.T) {
	store := &fakeStore{
		getOK:  true,
		getDep: Deployment{ID: testDeployID, WorkspaceID: testWorkspace, Status: StatusFailed, SourceKind: SourceImage, ImageRef: "x:latest"},
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})

	_, err := svc.RollbackToDeployment(authedCtx(), testDeployID)
	wantKind(t, err, problem.KindInvalidInput)
	if store.inserted.ServiceID != "" || store.insertedGit.ServiceID != "" {
		t.Error("rollback inserted a deployment for a non-healthy target")
	}
}

func TestRollbackToDeployment_DeniedWritesNothing(t *testing.T) {
	store := &fakeStore{
		getOK:  true,
		getDep: Deployment{ID: testDeployID, WorkspaceID: testWorkspace, Status: StatusRunning, SourceKind: SourceImage, ImageRef: "x:latest", ContainerPort: 80},
	}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)

	_, err := svc.RollbackToDeployment(authedCtx(), testDeployID)
	wantKind(t, err, problem.KindPermissionDenied)
	if store.inserted.ServiceID != "" || rec.called {
		t.Error("denied rollback wrote a deployment or an audit row")
	}
}

func TestRollbackToDeployment_NotFound(t *testing.T) {
	svc := newSvc(&fakeStore{getOK: false}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.RollbackToDeployment(authedCtx(), testDeployID)
	wantKind(t, err, problem.KindNotFound)
}

func TestEnqueueFirstDeployment_InsertsWithinTx(t *testing.T) {
	store := &fakeStore{svc: imageService(), svcOK: true}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	id, err := svc.EnqueueFirstDeployment(context.Background(), nil, testServiceID, testServerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != testDeployID {
		t.Errorf("id = %q, want the new deployment id", id)
	}
	if store.inserted.ServiceID != testServiceID || store.inserted.ImageRef != "traefik/whoami:latest" {
		t.Errorf("inserted = %+v, want the service's image", store.inserted)
	}
}

func TestEnqueueFirstDeployment_ServiceNotFound(t *testing.T) {
	store := &fakeStore{svcOK: false}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.EnqueueFirstDeployment(context.Background(), nil, testServiceID, testServerID)
	wantKind(t, err, problem.KindNotFound)
}

func TestPollDeployment_ClaimsJobWithServiceLabelAndNetwork(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		claimDep: Deployment{ID: testDeployID, ServiceID: testServiceID, EnvironmentID: testEnvID, ImageRef: "img:latest", ContainerPort: 8080},
		claimOK:  true,
		config: []ConfigForDeploy{
			// Environment-shared defaults; the service-level FOO overrides the env-shared one.
			{Type: "variable", Scope: "environment", Key: "FOO", Value: strptr("env-default")},
			{Type: "variable", Scope: "environment", Key: "SHARED", Value: strptr("from-env")},
			{Type: "variable", Scope: "service", Key: "FOO", Value: strptr("bar")},
			// A secret is decrypted at deploy time (fakeOpener strips the "sealed:" prefix).
			{Type: "secret", Scope: "service", Key: "TOKEN", Ciphertext: []byte("sealed:s3cr3t")},
		},
		svc: ServiceForDeploy{Slug: "api", Visibility: "private"}, svcOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})

	claimed, err := svc.PollDeployment(context.Background(), PollInput{AgentID: testAgentID, Credential: "plag_x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !claimed.HasWork || claimed.DeploymentID != testDeployID || claimed.ImageRef != "img:latest" || claimed.ContainerPort != 8080 {
		t.Errorf("claimed = %+v, want the queued job", claimed)
	}
	// The app label is the SERVICE id (route + container key), not the environment id.
	if claimed.AppLabel != testServiceID {
		t.Errorf("claimed = %+v, want the service app label", claimed)
	}
	// Service-level overrides environment-shared on a key collision; env-only keys pass
	// through; secrets are decrypted into plaintext.
	if claimed.Env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want service-level to override env-shared (bar)", claimed.Env["FOO"])
	}
	if claimed.Env["SHARED"] != "from-env" {
		t.Errorf("SHARED = %q, want env-shared default", claimed.Env["SHARED"])
	}
	if claimed.Env["TOKEN"] != "s3cr3t" {
		t.Errorf("TOKEN = %q, want decrypted secret plaintext", claimed.Env["TOKEN"])
	}
	if claimed.NetworkName != "plorigo-"+testEnvID || claimed.NetworkAlias != "api" || claimed.Visibility != "private" {
		t.Errorf("claimed = %+v, want per-env network + slug alias + visibility", claimed)
	}
	if len(store.events) != 1 || store.events[0].Status != StatusAssigned {
		t.Errorf("events = %+v, want one assigned event", store.events)
	}
}

func TestPollDeployment_NoWork(t *testing.T) {
	store := &fakeStore{credAgentID: testAgentID, credServerID: testServerID, credOK: true, claimOK: false}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	claimed, err := svc.PollDeployment(context.Background(), PollInput{AgentID: testAgentID, Credential: "plag_x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claimed.HasWork {
		t.Error("expected no work")
	}
}

func TestPollDeployment_UnknownCredential(t *testing.T) {
	store := &fakeStore{credOK: false}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.PollDeployment(context.Background(), PollInput{AgentID: testAgentID, Credential: "plag_bad"})
	wantKind(t, err, problem.KindPermissionDenied)
}

func TestPollDeployment_GitClaimCarriesSourceAndBuildTag(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		claimDep: Deployment{
			ID: testDeployID, ServiceID: testServiceID, EnvironmentID: testEnvID, ContainerPort: 8080,
			SourceKind: SourceGit, CloneURL: "https://github.com/o/r.git", GitRef: "main",
		},
		claimOK: true,
		svc:     ServiceForDeploy{Slug: "web", Visibility: "public"}, svcOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	claimed, err := svc.PollDeployment(context.Background(), PollInput{AgentID: testAgentID, Credential: "plag_x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claimed.SourceKind != SourceGit || claimed.CloneURL != "https://github.com/o/r.git" || claimed.GitRef != "main" {
		t.Errorf("claimed = %+v, want git source fields", claimed)
	}
	if claimed.BuiltImageTag != "plorigo-build:"+testDeployID {
		t.Errorf("built tag = %q, want deterministic per-deployment tag", claimed.BuiltImageTag)
	}
}

func TestPollDeployment_PreviewIsolatesNetworkAndWithholdsSecrets(t *testing.T) {
	const routeKey = testServiceID + "-pr-7"
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		claimDep: Deployment{
			ID: testDeployID, ServiceID: testServiceID, EnvironmentID: testEnvID, ContainerPort: 8080,
			Kind: KindPreview, RouteKey: routeKey,
			SourceKind: SourceGit, CloneURL: "https://github.com/o/r.git", GitRef: "feature",
		},
		claimOK: true,
		config: []ConfigForDeploy{
			{Type: "variable", Scope: "service", Key: "FOO", Value: strptr("bar")},
			{Type: "secret", Scope: "environment", Key: "TOKEN", Ciphertext: []byte("sealed:s3cr3t")},
		},
		svc: ServiceForDeploy{Slug: "web", Visibility: "public"}, svcOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})

	claimed, err := svc.PollDeployment(context.Background(), PollInput{AgentID: testAgentID, Credential: "plag_x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A preview routes and replaces by its own route_key, not the bare service id.
	if claimed.AppLabel != routeKey {
		t.Errorf("app label = %q, want the preview route_key %q", claimed.AppLabel, routeKey)
	}
	// It joins its OWN isolated network so it cannot reach production's siblings.
	if claimed.NetworkName != "plorigo-preview-"+routeKey {
		t.Errorf("network = %q, want the isolated preview network", claimed.NetworkName)
	}
	// Non-secret variables still flow; the environment's decrypted secrets are withheld.
	if claimed.Env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want non-secret variable injected", claimed.Env["FOO"])
	}
	if _, ok := claimed.Env["TOKEN"]; ok {
		t.Errorf("TOKEN present in preview env (%q); secrets must be withheld from previews", claimed.Env["TOKEN"])
	}
}

func TestReportDeployment_PreviewSupersedesByRouteKeyAndKeepsServiceURL(t *testing.T) {
	const routeKey = testServiceID + "-pr-7"
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{
			ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID, EnvironmentID: testEnvID,
			Kind: KindPreview, RouteKey: routeKey,
		},
		getOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID,
		Status: StatusRunning, HostPort: 32768, ContainerID: "abc",
		RouteURL: "http://" + routeKey + ".localhost:8083",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Supersede is keyed by the preview's route_key, so it never touches production.
	if !store.superseded || store.supersededWith != routeKey {
		t.Errorf("supersede = (%v, %q), want keyed by the preview route_key", store.superseded, store.supersededWith)
	}
	// A preview must NOT overwrite the service's cached live URL (that tracks production only).
	if store.routeServiceID != "" || store.routeURLReported != "" {
		t.Errorf("route cache = (%q, %q), want untouched for a preview", store.routeServiceID, store.routeURLReported)
	}
}

func TestReportDeployment_MismatchedServerDenied(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: otherServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID, Status: StatusRunning})
	wantKind(t, err, problem.KindPermissionDenied)
	if len(store.statusUpdates) != 0 {
		t.Error("a mismatched server must not update status")
	}
}

func TestReportDeployment_RunningUpdatesAndSupersedesByService(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID,
		Status: StatusRunning, HostPort: 32768, ContainerID: "abc", RouteURL: "http://svc.localhost:8083",
		LogLines: []string{"hello", "  "}, Message: "up",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.statusUpdates) != 1 || store.statusUpdates[0].Status != StatusRunning || store.statusUpdates[0].HostPort != 32768 {
		t.Errorf("status updates = %+v, want one running update with host port", store.statusUpdates)
	}
	if !store.superseded || store.supersededWith != testServiceID {
		t.Errorf("supersede = (%v, %q), want supersede keyed by the service", store.superseded, store.supersededWith)
	}
	if store.routeServiceID != testServiceID || store.routeURLReported != "http://svc.localhost:8083" {
		t.Errorf("route cache = (%q, %q), want the service URL cached", store.routeServiceID, store.routeURLReported)
	}
	statusEvents, logEvents := 0, 0
	for _, e := range store.events {
		switch e.Kind {
		case KindStatus:
			statusEvents++
		case KindLog:
			logEvents++
		}
	}
	if statusEvents != 1 || logEvents != 1 {
		t.Errorf("events = %+v, want 1 status + 1 log", store.events)
	}
}

func TestReportDeployment_PrivateRunningCachesNoRoute(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	// A private service reports running with no route URL: the service route cache stays untouched.
	if err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID,
		Status: StatusRunning, HostPort: 32768,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.routeServiceID != "" || store.routeURLReported != "" {
		t.Errorf("route cache = (%q, %q), want untouched for a private service", store.routeServiceID, store.routeURLReported)
	}
}

func TestReportDeployment_StampsStreamOnLogEventsOnly(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID,
		Status: StatusRunning, LogLines: []string{"serving on :8080", "  ", "GET / 200"},
		LogStream: StreamRuntime,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var statusStreams, logStreams []string
	for _, e := range store.events {
		switch e.Kind {
		case KindStatus:
			statusStreams = append(statusStreams, e.Stream)
		case KindLog:
			logStreams = append(logStreams, e.Stream)
		}
	}
	if len(statusStreams) != 1 || statusStreams[0] != "" {
		t.Errorf("status event streams = %v, want one empty stream", statusStreams)
	}
	if len(logStreams) != 2 || logStreams[0] != StreamRuntime || logStreams[1] != StreamRuntime {
		t.Errorf("log event streams = %v, want two %q", logStreams, StreamRuntime)
	}
}

func TestReportDeployment_RuntimeLogTickDoesNotSupersede(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID,
		Status: StatusRunning, HostPort: 0, LogLines: []string{"tick"}, LogStream: StreamRuntime,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.superseded {
		t.Error("a runtime-log tick (host port 0) must not supersede the previous deployment")
	}
	if len(store.statusUpdates) != 1 {
		t.Errorf("status updates = %d, want 1", len(store.statusUpdates))
	}
}

func TestReportDeployment_InvalidStatus(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID, Status: StatusQueued})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestReportDeployment_BuildPhasesAndCommitAccepted(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	if err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID, Status: StatusBuilding, Message: "building image",
	}); err != nil {
		t.Fatalf("building report rejected: %v", err)
	}
	if err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID, Status: StatusRouting, Message: "routing traffic",
	}); err != nil {
		t.Fatalf("routing report rejected: %v", err)
	}
	if err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID,
		Status: StatusRunning, HostPort: 32768, ContainerID: "abc",
		CommitSha: "deadbeef", BuiltImageRef: "plorigo-build:" + testDeployID,
	}); err != nil {
		t.Fatalf("running report rejected: %v", err)
	}
	last := store.statusUpdates[len(store.statusUpdates)-1]
	if last.CommitSha != "deadbeef" || last.BuiltImageRef != "plorigo-build:"+testDeployID {
		t.Errorf("status update = %+v, want commit + built image carried", last)
	}
}

func TestReportDeployment_HealthcheckAccepted(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	// healthcheck is the explicit phase the agent reports before probing the new container;
	// it must be accepted and persisted, and (carrying no host port) must not supersede.
	if err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID,
		Status: StatusHealthcheck, Message: "running health check",
	}); err != nil {
		t.Fatalf("healthcheck report rejected: %v", err)
	}
	if len(store.statusUpdates) != 1 || store.statusUpdates[0].Status != StatusHealthcheck {
		t.Fatalf("status updates = %+v, want one healthcheck update", store.statusUpdates)
	}
	if store.superseded {
		t.Fatalf("healthcheck superseded a deployment, want none (no host port)")
	}
}

func TestSyncRoutes_ReturnsVerifiedDomainsForAgentOwnedRoutes(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID}, getOK: true,
		verifiedDomains: map[string][]string{testServiceID: {"app.example.com", "api.example.com"}},
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	overrides, err := svc.SyncRoutes(context.Background(), SyncRoutesInput{
		AgentID: testAgentID, Credential: "plag_x",
		Routes: []ManagedRoute{{ServiceID: testServiceID, DeploymentID: testDeployID, HostPort: 32768}},
	})
	if err != nil {
		t.Fatalf("SyncRoutes: %v", err)
	}
	if len(overrides) != 1 || len(overrides[0].Hostnames) != 2 {
		t.Fatalf("overrides = %+v, want two custom hostnames", overrides)
	}
}

func TestReportRouteSync_MarksDomainsActiveOrFailed(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServiceID: testServiceID, ServerID: testServerID}, getOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	if err := svc.ReportRouteSync(context.Background(), ReportRouteSyncInput{
		AgentID: testAgentID, Credential: "plag_x",
		Results: []RouteSyncResult{{
			ServiceID: testServiceID, DeploymentID: testDeployID, Hostnames: []string{"app.example.com"}, OK: true,
		}},
	}); err != nil {
		t.Fatalf("ReportRouteSync active: %v", err)
	}
	if store.routeSyncStatus != "active" || store.routeSyncHosts[0] != "app.example.com" {
		t.Fatalf("route sync = status %q hosts %v, want active app.example.com", store.routeSyncStatus, store.routeSyncHosts)
	}
	if err := svc.ReportRouteSync(context.Background(), ReportRouteSyncInput{
		AgentID: testAgentID, Credential: "plag_x",
		Results: []RouteSyncResult{{
			ServiceID: testServiceID, DeploymentID: testDeployID, Hostnames: []string{"app.example.com"}, OK: false, Message: "reload failed",
		}},
	}); err != nil {
		t.Fatalf("ReportRouteSync failed: %v", err)
	}
	if store.routeSyncStatus != "failed" || store.routeSyncMessage != "reload failed" {
		t.Fatalf("route sync = status %q message %q, want failed reload failed", store.routeSyncStatus, store.routeSyncMessage)
	}
}

func TestValidateImageRef(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{in: "traefik/whoami", want: "traefik/whoami:latest"},
		{in: "nginx", want: "nginx:latest"},
		{in: "nginx:1.25", want: "nginx:1.25"},
		{in: "registry:5000/app", want: "registry:5000/app:latest"},
		{in: "img@sha256:abcdef", want: "img@sha256:abcdef"},
		{in: "  ", wantErr: true},
		{in: "has space", wantErr: true},
	}
	for _, c := range cases {
		got, err := validateImageRef(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("validateImageRef(%q) = %q, want error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("validateImageRef(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}
