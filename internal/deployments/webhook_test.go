package deployments

import (
	"testing"

	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

func gitPublicSvc() ServiceForDeploy {
	return ServiceForDeploy{
		EnvironmentID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace,
		SourceKind: SourceGit, SourceAccess: "public", Owner: "o", Repo: "r", ContainerPort: 3000, Slug: "web",
	}
}

func TestCreatePreviewForPR_ResolvesServerAndEnqueues(t *testing.T) {
	store := &fakeStore{
		svc: gitPublicSvc(), svcOK: true,
		latestServerID: testServerID, latestServerOK: true,
	}
	gh := fakeGitHub{pr: github.PullRequest{Number: 7, State: "open", HeadRef: "feat", HTMLURL: "https://github.com/o/r/pull/7"}}
	rec := &fakeRecorder{}
	svc := newSvcGH(store, gh, rec)

	depID, err := svc.CreatePreviewForPR(authedCtx(), testServiceID, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if depID != testDeployID {
		t.Errorf("deployment id = %q, want the inserted preview", depID)
	}
	p := store.insertedPreview
	if p.RouteKey != testServiceID+"-pr-7" || p.PRNumber != 7 || p.ServerID != testServerID || p.GitRef != "feat" {
		t.Errorf("inserted = %+v, want a PR-7 preview on the resolved server", p)
	}
	// The webhook actor is recorded, not a user.
	if !rec.called || rec.action != "deployment.preview" {
		t.Errorf("audit = (%v, %q), want deployment.preview", rec.called, rec.action)
	}
}

func TestCreatePreviewForPR_NoServerSkips(t *testing.T) {
	store := &fakeStore{svc: gitPublicSvc(), svcOK: true, latestServerOK: false}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	_, err := svc.CreatePreviewForPR(authedCtx(), testServiceID, 7)
	wantKind(t, err, problem.KindInvalidInput)
}

func TestTeardownPreviewForPR_EnqueuesForActivePreview(t *testing.T) {
	routeKey := testServiceID + "-pr-7"
	store := &fakeStore{
		activePreview: Deployment{
			ID: testDeployID, ServiceID: testServiceID, RouteKey: routeKey,
			EnvironmentID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace, ServerID: testServerID,
		},
		activePreviewOK: true,
	}
	rec := &fakeRecorder{}
	svc := newSvcGH(store, fakeGitHub{}, rec)

	if err := svc.TeardownPreviewForPR(authedCtx(), testServiceID, 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedTeardown.RouteKey != routeKey || store.insertedTeardown.DeploymentID != testDeployID {
		t.Errorf("teardown = %+v, want one keyed to the active preview", store.insertedTeardown)
	}
	if !rec.called || rec.action != "deployment.teardown" {
		t.Errorf("audit = (%v, %q), want deployment.teardown", rec.called, rec.action)
	}
}

func TestTeardownPreviewForPR_NoActivePreviewNoOp(t *testing.T) {
	store := &fakeStore{activePreviewOK: false}
	rec := &fakeRecorder{}
	svc := newSvcGH(store, fakeGitHub{}, rec)

	if err := svc.TeardownPreviewForPR(authedCtx(), testServiceID, 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedTeardown.RouteKey != "" || rec.called {
		t.Error("no active preview must be an idempotent no-op (no teardown enqueued, nothing audited)")
	}
}
