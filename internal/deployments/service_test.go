package deployments

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
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

	env map[string]string

	inserted    NewDeployment
	insertedGit NewDeploymentFromGit
	insertErr   error

	getDep Deployment
	getOK  bool

	claimDep Deployment
	claimOK  bool

	statusUpdates    []StatusUpdate
	events           []NewEvent
	supersededWith   string // service id passed to SupersedePreviousRunning
	superseded       bool
	routeServiceID   string
	routeURLReported string
	verifiedDomains  map[string][]string
	routeSyncStatus  string
	routeSyncMessage string
	routeSyncHosts   []string
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
func (f *fakeStore) EnvVarsForService(_ context.Context, _ string) (map[string]string, error) {
	return f.env, nil
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
func (f *fakeStore) SupersedePreviousRunning(_ context.Context, _ database.Tx, serviceID, _, _ string) error {
	f.superseded = true
	f.supersededWith = serviceID
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
	return newService(fakeTx{}, store, authorizer, rec, slog.Default())
}

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
		env:      map[string]string{"FOO": "bar"},
		svc:      ServiceForDeploy{Slug: "api", Visibility: "private"}, svcOK: true,
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
	if claimed.AppLabel != testServiceID || claimed.Env["FOO"] != "bar" {
		t.Errorf("claimed = %+v, want env + service app label", claimed)
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
