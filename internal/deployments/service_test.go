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
	testEnvID     = "11111111-1111-1111-1111-111111111111"
	testServerID  = "22222222-2222-2222-2222-222222222222"
	testProjectID = "33333333-3333-3333-3333-333333333333"
	testDeployID  = "44444444-4444-4444-4444-444444444444"
	testAgentID   = "55555555-5555-5555-5555-555555555555"
	otherServerID = "66666666-6666-6666-6666-666666666666"
	testWorkspace = "ws-1"
)

type fakeStore struct {
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

	inserted  NewDeployment
	insertErr error

	src         Source
	srcOK       bool
	insertedGit NewDeploymentFromGit

	getDep Deployment
	getOK  bool

	claimDep Deployment
	claimOK  bool

	statusUpdates []StatusUpdate
	events        []NewEvent
	superseded    bool
}

func (f *fakeStore) WorkspaceAndProjectForEnvironment(_ context.Context, _ string) (string, string, bool, error) {
	return f.envWs, f.envProj, f.envOK, f.envErr
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
func (f *fakeStore) EnvVarsForEnvironment(_ context.Context, _ string) (map[string]string, error) {
	return f.env, nil
}
func (f *fakeStore) InsertDeployment(_ context.Context, _ database.Tx, d NewDeployment) (Deployment, error) {
	f.inserted = d
	if f.insertErr != nil {
		return Deployment{}, f.insertErr
	}
	return Deployment{
		ID:            testDeployID,
		EnvironmentID: d.EnvironmentID,
		ProjectID:     d.ProjectID,
		WorkspaceID:   d.WorkspaceID,
		ServerID:      d.ServerID,
		ImageRef:      d.ImageRef,
		ContainerPort: d.ContainerPort,
		Status:        StatusQueued,
	}, nil
}
func (f *fakeStore) SourceForProject(_ context.Context, _ string) (Source, bool, error) {
	return f.src, f.srcOK, nil
}
func (f *fakeStore) InsertDeploymentFromGit(_ context.Context, _ database.Tx, d NewDeploymentFromGit) (Deployment, error) {
	f.insertedGit = d
	if f.insertErr != nil {
		return Deployment{}, f.insertErr
	}
	return Deployment{
		ID:            testDeployID,
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
func (f *fakeStore) SupersedePreviousRunning(_ context.Context, _ database.Tx, _, _, _ string) error {
	f.superseded = true
	return nil
}
func (f *fakeStore) AppendEvent(_ context.Context, _ database.Tx, e NewEvent) error {
	f.events = append(f.events, e)
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

func TestCreate_AuthorizedInsertsQueuedAndAudits(t *testing.T) {
	store := &fakeStore{envWs: testWorkspace, envProj: testProjectID, envOK: true, serverWs: testWorkspace, serverOK: true}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	dep, err := svc.Create(authedCtx(), CreateInput{EnvironmentID: testEnvID, ServerID: testServerID, ImageRef: "traefik/whoami", ContainerPort: 80})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dep.Status != StatusQueued {
		t.Errorf("status = %q, want %q", dep.Status, StatusQueued)
	}
	if store.inserted.ImageRef != "traefik/whoami:latest" {
		t.Errorf("image = %q, want :latest defaulted", store.inserted.ImageRef)
	}
	if store.inserted.ProjectID != testProjectID || store.inserted.WorkspaceID != testWorkspace {
		t.Errorf("inserted = %+v, want denormalized project/workspace", store.inserted)
	}
	if !rec.called || rec.action != "deployment.create" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCreate_DeniedWritesNothing(t *testing.T) {
	store := &fakeStore{envWs: testWorkspace, envProj: testProjectID, envOK: true}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)

	_, err := svc.Create(authedCtx(), CreateInput{EnvironmentID: testEnvID, ServerID: testServerID, ImageRef: "nginx", ContainerPort: 80})
	wantKind(t, err, problem.KindPermissionDenied)
	if store.inserted.ImageRef != "" {
		t.Error("a denied create must not insert a deployment")
	}
	if rec.called {
		t.Error("a denied create must not write an audit event")
	}
}

func TestCreate_InvalidImageRef(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{EnvironmentID: testEnvID, ServerID: testServerID, ImageRef: "  ", ContainerPort: 80})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreate_InvalidPort(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{EnvironmentID: testEnvID, ServerID: testServerID, ImageRef: "nginx", ContainerPort: 0})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreate_EnvironmentNotFound(t *testing.T) {
	store := &fakeStore{envOK: false}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{EnvironmentID: testEnvID, ServerID: testServerID, ImageRef: "nginx", ContainerPort: 80})
	wantKind(t, err, problem.KindNotFound)
}

func TestCreate_ServerInOtherWorkspaceNotFound(t *testing.T) {
	store := &fakeStore{envWs: testWorkspace, envProj: testProjectID, envOK: true, serverWs: "other-ws", serverOK: true}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{EnvironmentID: testEnvID, ServerID: testServerID, ImageRef: "nginx", ContainerPort: 80})
	wantKind(t, err, problem.KindNotFound)
	if store.inserted.ImageRef != "" {
		t.Error("a cross-workspace server must not insert a deployment")
	}
}

func TestPollDeployment_ClaimsJobWithEnv(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		claimDep: Deployment{ID: testDeployID, EnvironmentID: testEnvID, ImageRef: "img:latest", ContainerPort: 8080},
		claimOK:  true,
		env:      map[string]string{"FOO": "bar"},
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})

	claimed, err := svc.PollDeployment(context.Background(), PollInput{AgentID: testAgentID, Credential: "plag_x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !claimed.HasWork || claimed.DeploymentID != testDeployID || claimed.ImageRef != "img:latest" || claimed.ContainerPort != 8080 {
		t.Errorf("claimed = %+v, want the queued job", claimed)
	}
	if claimed.AppLabel != testEnvID || claimed.Env["FOO"] != "bar" {
		t.Errorf("claimed = %+v, want env + app label", claimed)
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

func TestReportDeployment_MismatchedServerDenied(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServerID: otherServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID, Status: StatusRunning})
	wantKind(t, err, problem.KindPermissionDenied)
	if len(store.statusUpdates) != 0 {
		t.Error("a mismatched server must not update status")
	}
}

func TestReportDeployment_RunningUpdatesAndSupersedes(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{
		AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID,
		Status: StatusRunning, HostPort: 32768, ContainerID: "abc", LogLines: []string{"hello", "  "}, Message: "up",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.statusUpdates) != 1 || store.statusUpdates[0].Status != StatusRunning || store.statusUpdates[0].HostPort != 32768 {
		t.Errorf("status updates = %+v, want one running update with host port", store.statusUpdates)
	}
	if !store.superseded {
		t.Error("reaching running must supersede the previous running deployment")
	}
	// one status event + one log event (the blank log line is skipped).
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

func TestReportDeployment_StampsStreamOnLogEventsOnly(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	// A runtime-log tick: status running, two log lines (one blank, skipped), runtime stream.
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
	// The status event is stream-less; each (non-blank) log line carries the report's stream.
	if len(statusStreams) != 1 || statusStreams[0] != "" {
		t.Errorf("status event streams = %v, want one empty stream", statusStreams)
	}
	if len(logStreams) != 2 || logStreams[0] != StreamRuntime || logStreams[1] != StreamRuntime {
		t.Errorf("log event streams = %v, want two %q", logStreams, StreamRuntime)
	}
}

func TestReportDeployment_RuntimeLogTickDoesNotSupersede(t *testing.T) {
	// The tail loop re-reports status=running with host port 0 just to attach log lines.
	// That must not re-run the supersede (which belongs to the real "now running" report).
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServerID: testServerID, EnvironmentID: testEnvID},
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
	// It still records the status update and the log line.
	if len(store.statusUpdates) != 1 {
		t.Errorf("status updates = %d, want 1", len(store.statusUpdates))
	}
}

func TestReportDeployment_InvalidStatus(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	err := svc.ReportDeployment(context.Background(), ReportInput{AgentID: testAgentID, Credential: "plag_x", DeploymentID: testDeployID, Status: StatusQueued})
	wantKind(t, err, problem.KindInvalidInput)
}

func TestCreateFromSource_PublicInsertsGitDeploymentAndAudits(t *testing.T) {
	store := &fakeStore{
		envWs: testWorkspace, envProj: testProjectID, envOK: true,
		serverWs: testWorkspace, serverOK: true,
		src:   Source{CloneURL: "https://github.com/o/r.git", Branch: "feature", DefaultBranch: "main", Access: "public"},
		srcOK: true,
	}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	dep, err := svc.CreateFromSource(authedCtx(), CreateFromSourceInput{EnvironmentID: testEnvID, ServerID: testServerID, ContainerPort: 80})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dep.Status != StatusQueued || dep.SourceKind != SourceGit {
		t.Errorf("dep = %+v, want queued git deployment", dep)
	}
	if store.insertedGit.CloneURL != "https://github.com/o/r.git" || store.insertedGit.SourceAccess != "public" {
		t.Errorf("inserted = %+v, want public clone url", store.insertedGit)
	}
	// No explicit ref: defaults to the source's selected branch.
	if store.insertedGit.GitRef != "feature" {
		t.Errorf("git_ref = %q, want the source branch defaulted", store.insertedGit.GitRef)
	}
	if store.insertedGit.ProjectID != testProjectID || store.insertedGit.WorkspaceID != testWorkspace {
		t.Errorf("inserted = %+v, want denormalized project/workspace", store.insertedGit)
	}
	if !rec.called || rec.action != "deployment.create" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCreateFromSource_DefaultsRefToDefaultBranchWhenNoBranch(t *testing.T) {
	store := &fakeStore{
		envWs: testWorkspace, envProj: testProjectID, envOK: true,
		serverWs: testWorkspace, serverOK: true,
		src:   Source{CloneURL: "https://github.com/o/r.git", DefaultBranch: "main", Access: "public"},
		srcOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.CreateFromSource(authedCtx(), CreateFromSourceInput{EnvironmentID: testEnvID, ServerID: testServerID, ContainerPort: 80}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedGit.GitRef != "main" {
		t.Errorf("git_ref = %q, want default branch", store.insertedGit.GitRef)
	}
}

func TestCreateFromSource_RejectsPrivateSource(t *testing.T) {
	store := &fakeStore{
		envWs: testWorkspace, envProj: testProjectID, envOK: true,
		serverWs: testWorkspace, serverOK: true,
		src:   Source{CloneURL: "https://github.com/o/r.git", Branch: "main", Access: "oauth"},
		srcOK: true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateFromSource(authedCtx(), CreateFromSourceInput{EnvironmentID: testEnvID, ServerID: testServerID, ContainerPort: 80})
	wantKind(t, err, problem.KindInvalidInput)
	if store.insertedGit.CloneURL != "" {
		t.Error("a private source must not insert a deployment (public-first)")
	}
}

func TestCreateFromSource_NoSourceNotFound(t *testing.T) {
	store := &fakeStore{envWs: testWorkspace, envProj: testProjectID, envOK: true, serverWs: testWorkspace, serverOK: true, srcOK: false}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateFromSource(authedCtx(), CreateFromSourceInput{EnvironmentID: testEnvID, ServerID: testServerID, ContainerPort: 80})
	wantKind(t, err, problem.KindNotFound)
}

func TestCreateFromSource_DeniedWritesNothing(t *testing.T) {
	store := &fakeStore{envWs: testWorkspace, envProj: testProjectID, envOK: true, srcOK: true, src: Source{Access: "public"}}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	_, err := svc.CreateFromSource(authedCtx(), CreateFromSourceInput{EnvironmentID: testEnvID, ServerID: testServerID, ContainerPort: 80})
	wantKind(t, err, problem.KindPermissionDenied)
	if store.insertedGit.CloneURL != "" || rec.called {
		t.Error("a denied create must not insert or audit")
	}
}

func TestPollDeployment_GitClaimCarriesSourceAndBuildTag(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		claimDep: Deployment{
			ID: testDeployID, EnvironmentID: testEnvID, ContainerPort: 8080,
			SourceKind: SourceGit, CloneURL: "https://github.com/o/r.git", GitRef: "main",
		},
		claimOK: true,
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

func TestReportDeployment_BuildPhasesAndCommitAccepted(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getDep: Deployment{ID: testDeployID, ServerID: testServerID, EnvironmentID: testEnvID},
		getOK:  true,
	}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	// 'building' and 'routing' are valid agent-reported statuses.
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
	// 'running' with the built image + commit threads them into the status update.
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
