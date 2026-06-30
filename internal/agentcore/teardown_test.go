package agentcore

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
)

// fakeTeardownClient captures the agent's teardown reports.
type fakeTeardownClient struct {
	reports []*agentv1.ReportTeardownJobRequest
}

func (f *fakeTeardownClient) PollTeardownJob(_ context.Context, _ *connect.Request[agentv1.PollTeardownJobRequest]) (*connect.Response[agentv1.PollTeardownJobResponse], error) {
	return connect.NewResponse(&agentv1.PollTeardownJobResponse{}), nil
}

func (f *fakeTeardownClient) ReportTeardownJob(_ context.Context, req *connect.Request[agentv1.ReportTeardownJobRequest]) (*connect.Response[agentv1.ReportTeardownJobResponse], error) {
	f.reports = append(f.reports, req.Msg)
	return connect.NewResponse(&agentv1.ReportTeardownJobResponse{}), nil
}

// fakeTeardownRuntime fakes the Docker surface a teardown needs.
type fakeTeardownRuntime struct {
	removedLabel   string
	removeCount    int
	removeErr      error
	routes         []managedRoute
	routesErr      error
	removedNetwork string
}

func (f *fakeTeardownRuntime) removeByService(_ context.Context, appLabel string, emit func(string)) (int, error) {
	f.removedLabel = appLabel
	emit("removing preview container abc")
	return f.removeCount, f.removeErr
}

func (f *fakeTeardownRuntime) listManagedRoutes(_ context.Context) ([]managedRoute, error) {
	return f.routes, f.routesErr
}

func (f *fakeTeardownRuntime) removeNetwork(_ context.Context, name string) error {
	f.removedNetwork = name
	return nil
}

func teardownIdent() *identity { return &identity{st: state{AgentID: "agent-1", Credential: "cred"}} }

func statuses(reports []*agentv1.ReportTeardownJobRequest) []string {
	out := make([]string, 0, len(reports))
	for _, r := range reports {
		out = append(out, r.GetStatus())
	}
	return out
}

func TestExecuteTeardown_RemovesContainerReconcilesAndSucceeds(t *testing.T) {
	// A sibling production route remains after the preview is gone, so the reconcile re-applies it
	// (and proves apply ran from Docker truth — the torn-down preview is absent).
	remaining := []managedRoute{{ServiceID: "prod-svc", DeploymentID: "d1", ContainerID: "c1", HostPort: 8080}}
	rt := &fakeTeardownRuntime{removeCount: 2, routes: remaining}
	router := &fakeRouter{}
	client := &fakeTeardownClient{}
	job := &agentv1.PollTeardownJobResponse{TeardownId: "td-1", RouteKey: "svc-pr-7", NetworkName: "plorigo-preview-svc-pr-7"}

	executeTeardown(context.Background(), &strings.Builder{}, client, teardownIdent(), rt, router, job)

	if rt.removedLabel != "svc-pr-7" {
		t.Errorf("removed label = %q, want the route_key", rt.removedLabel)
	}
	if rt.removedNetwork != "plorigo-preview-svc-pr-7" {
		t.Errorf("removed network = %q, want the preview network", rt.removedNetwork)
	}
	if len(router.routes) != 1 || router.routes[0].ServiceID != "prod-svc" {
		t.Errorf("reconciled routes = %+v, want the remaining sibling route from Docker truth", router.routes)
	}
	got := statuses(client.reports)
	want := []string{teardownStatusStopping, teardownStatusRemoving, teardownStatusSucceeded}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
}

// An already-removed preview (0 containers) still reconciles Caddy and reports success — teardown
// is idempotent.
func TestExecuteTeardown_IdempotentWhenAlreadyGone(t *testing.T) {
	rt := &fakeTeardownRuntime{removeCount: 0}
	client := &fakeTeardownClient{}
	job := &agentv1.PollTeardownJobResponse{TeardownId: "td-2", RouteKey: "svc-pr-7"}

	executeTeardown(context.Background(), &strings.Builder{}, client, teardownIdent(), rt, &fakeRouter{}, job)

	last := client.reports[len(client.reports)-1]
	if last.GetStatus() != teardownStatusSucceeded {
		t.Fatalf("final status = %q, want succeeded", last.GetStatus())
	}
	if !strings.Contains(last.GetMessage(), "already removed") {
		t.Errorf("message = %q, want it to note the preview was already removed", last.GetMessage())
	}
}

func TestExecuteTeardown_DockerUnavailableFails(t *testing.T) {
	client := &fakeTeardownClient{}
	job := &agentv1.PollTeardownJobResponse{TeardownId: "td-3", RouteKey: "svc-pr-7"}

	executeTeardown(context.Background(), &strings.Builder{}, client, teardownIdent(), nil, &fakeRouter{}, job)

	if len(client.reports) != 1 || client.reports[0].GetStatus() != teardownStatusFailed {
		t.Fatalf("reports = %v, want a single failed report when Docker is unavailable", statuses(client.reports))
	}
}

func TestExecuteTeardown_RemoveErrorFails(t *testing.T) {
	rt := &fakeTeardownRuntime{removeErr: context.DeadlineExceeded}
	client := &fakeTeardownClient{}
	job := &agentv1.PollTeardownJobResponse{TeardownId: "td-4", RouteKey: "svc-pr-7"}

	executeTeardown(context.Background(), &strings.Builder{}, client, teardownIdent(), rt, &fakeRouter{}, job)

	last := client.reports[len(client.reports)-1]
	if last.GetStatus() != teardownStatusFailed {
		t.Fatalf("final status = %q, want failed when the container can't be removed", last.GetStatus())
	}
	for _, r := range client.reports {
		if r.GetStatus() == teardownStatusSucceeded {
			t.Error("a teardown that failed to remove the container must not report success")
		}
	}
}
