package deployments

import (
	"testing"

	"github.com/plorigo/plorigo/internal/platform/problem"
)

// previewDeployRow is a stored preview deployment the teardown tests act on.
func previewDeployRow() Deployment {
	return Deployment{
		ID:            testDeployID,
		ServiceID:     testServiceID,
		Kind:          KindPreview,
		RouteKey:      testServiceID + "-pr-7",
		EnvironmentID: testEnvID,
		ProjectID:     testProjectID,
		WorkspaceID:   testWorkspace,
		ServerID:      testServerID,
		Status:        StatusRunning,
	}
}

func TestTeardownPreview_EnqueuesForPreview(t *testing.T) {
	store := &fakeStore{getDep: previewDeployRow(), getOK: true}
	rec := &fakeRecorder{}
	svc := newSvcGH(store, fakeGitHub{}, rec)

	job, err := svc.TeardownPreview(authedCtx(), testDeployID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Status != TeardownStatusQueued {
		t.Errorf("status = %q, want queued", job.Status)
	}
	tj := store.insertedTeardown
	if tj.RouteKey != testServiceID+"-pr-7" || tj.ServerID != testServerID || tj.DeploymentID != testDeployID {
		t.Errorf("inserted = %+v, want the preview's route_key/server/deployment", tj)
	}
	if !rec.called || rec.action != "deployment.teardown" {
		t.Errorf("audit = (%v, %q), want deployment.teardown", rec.called, rec.action)
	}
}

func TestTeardownPreview_FallsBackToServiceIDRouteKey(t *testing.T) {
	dep := previewDeployRow()
	dep.RouteKey = "" // an older preview row predating route_key
	store := &fakeStore{getDep: dep, getOK: true}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	if _, err := svc.TeardownPreview(authedCtx(), testDeployID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.insertedTeardown.RouteKey != testServiceID {
		t.Errorf("route_key = %q, want the service id fallback", store.insertedTeardown.RouteKey)
	}
}

func TestTeardownPreview_RejectsProduction(t *testing.T) {
	dep := previewDeployRow()
	dep.Kind = KindProduction
	store := &fakeStore{getDep: dep, getOK: true}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	_, err := svc.TeardownPreview(authedCtx(), testDeployID)
	wantKind(t, err, problem.KindInvalidInput)
}

func TestTeardownPreview_NotFound(t *testing.T) {
	store := &fakeStore{getOK: false}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	_, err := svc.TeardownPreview(authedCtx(), testDeployID)
	wantKind(t, err, problem.KindNotFound)
}

func TestTeardownPreview_Unauthorized(t *testing.T) {
	store := &fakeStore{getDep: previewDeployRow(), getOK: true}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})

	_, err := svc.TeardownPreview(authedCtx(), testDeployID)
	wantKind(t, err, problem.KindPermissionDenied)
	if store.insertedTeardown.RouteKey != "" {
		t.Error("a denied teardown must not enqueue a job")
	}
}

func TestPollTeardownJob_ClaimsAndComputesNetwork(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		claimTeardown:   TeardownJob{ID: testTeardownID, RouteKey: testServiceID + "-pr-7", ServerID: testServerID},
		claimTeardownOK: true,
	}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	claimed, err := svc.PollTeardownJob(authedCtx(), PollInput{AgentID: testAgentID, Credential: "cred"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !claimed.HasWork || claimed.TeardownID != testTeardownID {
		t.Fatalf("claimed = %+v, want work for the teardown", claimed)
	}
	if claimed.NetworkName != "plorigo-preview-"+testServiceID+"-pr-7" {
		t.Errorf("network = %q, want the preview network for the route_key", claimed.NetworkName)
	}
}

func TestPollTeardownJob_NoWork(t *testing.T) {
	store := &fakeStore{credAgentID: testAgentID, credServerID: testServerID, credOK: true, claimTeardownOK: false}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	claimed, err := svc.PollTeardownJob(authedCtx(), PollInput{AgentID: testAgentID, Credential: "cred"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claimed.HasWork {
		t.Errorf("claimed.HasWork = true, want false when the queue is empty")
	}
}

func TestReportTeardownJob_SuccessMarksTornDown(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getTeardown:   TeardownJob{ID: testTeardownID, RouteKey: testServiceID + "-pr-7", ServerID: testServerID},
		getTeardownOK: true,
	}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	err := svc.ReportTeardownJob(authedCtx(), ReportTeardownInput{
		AgentID: testAgentID, Credential: "cred", TeardownID: testTeardownID, Status: TeardownStatusSucceeded,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.teardownStatusUpdates) != 1 || store.teardownStatusUpdates[0].Status != TeardownStatusSucceeded {
		t.Errorf("status updates = %+v, want one succeeded transition", store.teardownStatusUpdates)
	}
	if store.tornDownRouteKey != testServiceID+"-pr-7" || store.tornDownServerID != testServerID {
		t.Errorf("marked torndown = (%q, %q), want the preview's route_key/server", store.tornDownRouteKey, store.tornDownServerID)
	}
}

func TestReportTeardownJob_RejectsForeignServer(t *testing.T) {
	store := &fakeStore{
		credAgentID: testAgentID, credServerID: testServerID, credOK: true,
		getTeardown:   TeardownJob{ID: testTeardownID, ServerID: otherServerID},
		getTeardownOK: true,
	}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	err := svc.ReportTeardownJob(authedCtx(), ReportTeardownInput{
		AgentID: testAgentID, Credential: "cred", TeardownID: testTeardownID, Status: TeardownStatusSucceeded,
	})
	wantKind(t, err, problem.KindPermissionDenied)
	if store.tornDownRouteKey != "" {
		t.Error("a foreign-server report must not mark anything torn down")
	}
}

func TestReportTeardownJob_RejectsInvalidStatus(t *testing.T) {
	store := &fakeStore{credAgentID: testAgentID, credServerID: testServerID, credOK: true}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	err := svc.ReportTeardownJob(authedCtx(), ReportTeardownInput{
		AgentID: testAgentID, Credential: "cred", TeardownID: testTeardownID, Status: TeardownStatusQueued,
	})
	wantKind(t, err, problem.KindInvalidInput)
}
