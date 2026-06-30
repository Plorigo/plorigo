package webhooks

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type fakeStore struct {
	workspaceID string
	wsOK        bool
	wsErr       error
	services    []string
	svcErr      error
}

func (f *fakeStore) WorkspaceForInstallation(_ context.Context, _ string) (string, bool, error) {
	return f.workspaceID, f.wsOK, f.wsErr
}
func (f *fakeStore) ServicesForRepo(_ context.Context, _, _, _ string) ([]string, error) {
	return f.services, f.svcErr
}

type recordedCall struct {
	serviceID string
	pr        int32
}

type fakeCreator struct {
	calls []recordedCall
	err   error
}

func (f *fakeCreator) CreatePreviewForPR(_ context.Context, serviceID string, pr int32) (string, error) {
	f.calls = append(f.calls, recordedCall{serviceID, pr})
	return "dep-" + serviceID, f.err
}

type fakeTeardowner struct {
	calls []recordedCall
	err   error
}

func (f *fakeTeardowner) TeardownPreviewForPR(_ context.Context, serviceID string, pr int32) error {
	f.calls = append(f.calls, recordedCall{serviceID, pr})
	return f.err
}

func newWebhookSvc(store Store, c PreviewCreator, td PreviewTeardowner) *service {
	return newService(store, c, td, slog.Default())
}

func openedEvent() PullRequestEvent {
	return PullRequestEvent{Action: ActionOpened, InstallationID: "42", Owner: "o", Repo: "r", PRNumber: 7}
}

func TestHandlePullRequest_OpenedCreatesPerService(t *testing.T) {
	store := &fakeStore{workspaceID: "ws", wsOK: true, services: []string{"s1", "s2"}}
	c := &fakeCreator{}
	td := &fakeTeardowner{}

	res, err := newWebhookSvc(store, c, td).HandlePullRequest(context.Background(), openedEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.MatchedServices != 2 || res.Created != 2 || res.TornDown != 0 {
		t.Fatalf("result = %+v, want 2 matched + 2 created", res)
	}
	if len(c.calls) != 2 || c.calls[0].pr != 7 || c.calls[1].serviceID != "s2" {
		t.Errorf("create calls = %+v, want one per service with pr 7", c.calls)
	}
	if len(td.calls) != 0 {
		t.Errorf("teardown calls = %+v, want none on open", td.calls)
	}
}

func TestHandlePullRequest_ClosedTearsDown(t *testing.T) {
	store := &fakeStore{workspaceID: "ws", wsOK: true, services: []string{"s1"}}
	c := &fakeCreator{}
	td := &fakeTeardowner{}
	e := openedEvent()
	e.Action = ActionClosed
	e.Merged = true

	res, err := newWebhookSvc(store, c, td).HandlePullRequest(context.Background(), e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TornDown != 1 || res.Created != 0 {
		t.Fatalf("result = %+v, want 1 torn down", res)
	}
	if len(td.calls) != 1 || td.calls[0].serviceID != "s1" || td.calls[0].pr != 7 {
		t.Errorf("teardown calls = %+v, want s1/pr-7", td.calls)
	}
	if len(c.calls) != 0 {
		t.Errorf("create calls = %+v, want none on close", c.calls)
	}
}

func TestHandlePullRequest_UnknownInstallationIgnored(t *testing.T) {
	store := &fakeStore{wsOK: false}
	c := &fakeCreator{}
	td := &fakeTeardowner{}

	res, err := newWebhookSvc(store, c, td).HandlePullRequest(context.Background(), openedEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ignored == "" || res.Created != 0 || len(c.calls) != 0 {
		t.Errorf("result = %+v, want ignored with no side effects for an unknown installation", res)
	}
}

func TestHandlePullRequest_NoMatchingServiceIgnored(t *testing.T) {
	store := &fakeStore{workspaceID: "ws", wsOK: true, services: nil}
	c := &fakeCreator{}

	res, err := newWebhookSvc(store, c, &fakeTeardowner{}).HandlePullRequest(context.Background(), openedEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ignored == "" || len(c.calls) != 0 {
		t.Errorf("result = %+v, want ignored when no service matches the repo", res)
	}
}

func TestHandlePullRequest_UnhandledActionIgnored(t *testing.T) {
	store := &fakeStore{workspaceID: "ws", wsOK: true, services: []string{"s1"}}
	c := &fakeCreator{}
	e := openedEvent()
	e.Action = "labeled"

	res, err := newWebhookSvc(store, c, &fakeTeardowner{}).HandlePullRequest(context.Background(), e)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ignored == "" || len(c.calls) != 0 {
		t.Errorf("result = %+v, want an unhandled action ignored before any lookup", res)
	}
}

// A per-service failure is logged and skipped — it neither aborts the delivery nor returns an error
// (which would make GitHub redeliver).
func TestHandlePullRequest_PerServiceFailureContinues(t *testing.T) {
	store := &fakeStore{workspaceID: "ws", wsOK: true, services: []string{"s1", "s2"}}
	c := &fakeCreator{err: errors.New("boom")}

	res, err := newWebhookSvc(store, c, &fakeTeardowner{}).HandlePullRequest(context.Background(), openedEvent())
	if err != nil {
		t.Fatalf("a per-service failure must not fail the delivery: %v", err)
	}
	if res.Created != 0 || len(c.calls) != 2 {
		t.Errorf("result = %+v, calls = %d; want both attempted, none counted created", res, len(c.calls))
	}
}
