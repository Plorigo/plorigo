package deployments

import (
	"testing"
	"time"
)

func expiredPreviewRow(id string) Deployment {
	return Deployment{
		ID: id, ServiceID: testServiceID, Kind: KindPreview, RouteKey: testServiceID + "-pr-7",
		EnvironmentID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace, ServerID: testServerID,
		Status: StatusRunning,
	}
}

func TestExpirePreviews_EnqueuesTeardownPerExpired(t *testing.T) {
	store := &fakeStore{expiredPreviews: []Deployment{expiredPreviewRow("d1"), expiredPreviewRow("d2")}}
	rec := &fakeRecorder{}
	svc := newSvcGH(store, fakeGitHub{}, rec)

	n, err := svc.ExpirePreviews(authedCtx(), 72*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("enqueued = %d, want 2", n)
	}
	// The last enqueue is captured by the fake; assert it is a teardown for an expired preview,
	// audited as an expiry.
	if store.insertedTeardown.DeploymentID == "" || store.insertedTeardown.RouteKey != testServiceID+"-pr-7" {
		t.Errorf("teardown = %+v, want one for the expired preview", store.insertedTeardown)
	}
	if !rec.called || rec.action != "deployment.expire" {
		t.Errorf("audit = (%v, %q), want deployment.expire", rec.called, rec.action)
	}
}

func TestExpirePreviews_DisabledWhenTTLZero(t *testing.T) {
	store := &fakeStore{expiredPreviews: []Deployment{expiredPreviewRow("d1")}}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	n, err := svc.ExpirePreviews(authedCtx(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 || store.insertedTeardown.DeploymentID != "" {
		t.Error("ttl <= 0 must disable expiry (no scan, no teardown)")
	}
}

func TestExpirePreviews_NoneExpired(t *testing.T) {
	store := &fakeStore{expiredPreviews: nil}
	svc := newSvcGH(store, fakeGitHub{}, &fakeRecorder{})

	n, err := svc.ExpirePreviews(authedCtx(), time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("enqueued = %d, want 0 when nothing is expired", n)
	}
}
