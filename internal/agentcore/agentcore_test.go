package agentcore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
)

// fakeAgentClient rejects the old credential, accepts a registration with the new
// token, then accepts heartbeats with the rotated credential — the exact sequence of a
// server that was deleted in the dashboard and re-connected with a fresh token.
type fakeAgentClient struct {
	registerCalls atomic.Int32
	healed        chan struct{}
}

func (f *fakeAgentClient) Register(_ context.Context, req *connect.Request[agentv1.RegisterRequest]) (*connect.Response[agentv1.RegisterResponse], error) {
	f.registerCalls.Add(1)
	if req.Msg.GetRegistrationToken() != "plrt_new" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("registration token is invalid or expired"))
	}
	return connect.NewResponse(&agentv1.RegisterResponse{AgentId: "agent-2", Credential: "plag_new"}), nil
}

func (f *fakeAgentClient) Heartbeat(_ context.Context, req *connect.Request[agentv1.HeartbeatRequest]) (*connect.Response[agentv1.HeartbeatResponse], error) {
	if req.Msg.GetCredential() != "plag_new" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("unknown agent credential"))
	}
	select {
	case f.healed <- struct{}{}:
	default:
	}
	return connect.NewResponse(&agentv1.HeartbeatResponse{}), nil
}

func TestHeartbeatLoop_SelfHealsWithProvidedToken(t *testing.T) {
	dir := t.TempDir()
	client := &fakeAgentClient{healed: make(chan struct{}, 1)}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_stale"}}
	opts := Options{
		RegistrationToken: "plrt_new",
		DataDir:           dir,
		HeartbeatInterval: time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var out strings.Builder
	go func() { done <- heartbeatLoop(ctx, &out, client, ident, opts) }()

	select {
	case <-client.healed:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatalf("loop never heartbeat with the rotated credential; output:\n%s", out.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("heartbeatLoop returned error: %v", err)
	}

	if got := ident.get(); got.AgentID != "agent-2" || got.Credential != "plag_new" {
		t.Errorf("identity = %+v, want rotated to agent-2/plag_new", got)
	}
	if calls := client.registerCalls.Load(); calls != 1 {
		t.Errorf("register calls = %d, want exactly 1 (token is single-use)", calls)
	}
	// The rotated identity is persisted, so a restart resumes as the NEW agent.
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("read persisted state: %v", err)
	}
	if !strings.Contains(string(data), "agent-2") {
		t.Errorf("persisted state = %s, want the rotated agent id", data)
	}
}

func TestHeartbeatLoop_NoTokenKeepsBackingOff(t *testing.T) {
	// Without a token there is nothing to self-heal with: the loop must keep retrying
	// (never re-register, never exit) until cancelled.
	client := &fakeAgentClient{healed: make(chan struct{}, 1)}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_stale"}}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := heartbeatLoop(ctx, io.Discard, client, ident, Options{HeartbeatInterval: time.Millisecond}); err != nil {
		t.Fatalf("heartbeatLoop returned error: %v", err)
	}
	if calls := client.registerCalls.Load(); calls != 0 {
		t.Errorf("register calls = %d, want 0 without a token", calls)
	}
	if got := ident.get(); got.Credential != "plag_stale" {
		t.Errorf("identity = %+v, want unchanged", got)
	}
}

type fakeDeployClient struct {
	reports []*agentv1.ReportDeploymentRequest
}

func (f *fakeDeployClient) PollDeployment(_ context.Context, _ *connect.Request[agentv1.PollDeploymentRequest]) (*connect.Response[agentv1.PollDeploymentResponse], error) {
	return connect.NewResponse(&agentv1.PollDeploymentResponse{}), nil
}

func (f *fakeDeployClient) ReportDeployment(_ context.Context, req *connect.Request[agentv1.ReportDeploymentRequest]) (*connect.Response[agentv1.ReportDeploymentResponse], error) {
	f.reports = append(f.reports, req.Msg)
	return connect.NewResponse(&agentv1.ReportDeploymentResponse{}), nil
}

type fakeRuntime struct {
	ops         []string
	containerID string
	hostPort    int32
	runErr      error
	replaceErr  error
	removeErr   error
	replaceKeep string
	removed     []string
}

func (f *fakeRuntime) pull(_ context.Context, _ string, emit func(string)) error {
	f.ops = append(f.ops, "pull")
	emit("pull complete")
	return nil
}

func (f *fakeRuntime) run(_ context.Context, _ runInput) (string, int32, error) {
	f.ops = append(f.ops, "run")
	return f.containerID, f.hostPort, f.runErr
}

func (f *fakeRuntime) replacePreviousExcept(_ context.Context, _ string, keepID string, emit func(string)) error {
	f.ops = append(f.ops, "replace")
	f.replaceKeep = keepID
	emit("removed old")
	return f.replaceErr
}

func (f *fakeRuntime) removeContainer(_ context.Context, containerID string, emit func(string)) error {
	f.ops = append(f.ops, "remove")
	f.removed = append(f.removed, containerID)
	emit("removed failed")
	return f.removeErr
}

func (f *fakeRuntime) recentLogs(_ context.Context, _ string, _ int) []string {
	f.ops = append(f.ops, "logs")
	return []string{"ready"}
}

func TestExecuteDeployment_RetiresPreviousOnlyAfterNewContainerIsHealthy(t *testing.T) {
	oldHealthCheck := runHealthCheck
	defer func() { runHealthCheck = oldHealthCheck }()

	runtime := &fakeRuntime{containerID: "new-container", hostPort: 32768}
	runHealthCheck = func(_ context.Context, _ int32) error {
		runtime.ops = append(runtime.ops, "health")
		return nil
	}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork:       true,
		DeploymentId:  "dep-1",
		ImageRef:      "traefik/whoami:latest",
		ContainerPort: 80,
		AppLabel:      "env-1",
	})

	wantOps := []string{"pull", "run", "health", "replace", "logs"}
	if !reflect.DeepEqual(runtime.ops, wantOps) {
		t.Fatalf("ops = %v, want %v", runtime.ops, wantOps)
	}
	if runtime.replaceKeep != "new-container" {
		t.Fatalf("replace kept %q, want new container", runtime.replaceKeep)
	}
	if len(runtime.removed) != 0 {
		t.Fatalf("removed failed containers = %v, want none on success", runtime.removed)
	}
	if got := deploy.reports[len(deploy.reports)-1].GetStatus(); got != statusRunning {
		t.Fatalf("last report status = %q, want %q", got, statusRunning)
	}
}

func TestExecuteDeployment_HealthFailureDoesNotRetirePreviousContainer(t *testing.T) {
	oldHealthCheck := runHealthCheck
	defer func() { runHealthCheck = oldHealthCheck }()

	runtime := &fakeRuntime{containerID: "new-container", hostPort: 32768}
	runHealthCheck = func(_ context.Context, _ int32) error {
		runtime.ops = append(runtime.ops, "health")
		return errors.New("not listening")
	}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork:       true,
		DeploymentId:  "dep-1",
		ImageRef:      "traefik/whoami:latest",
		ContainerPort: 80,
		AppLabel:      "env-1",
	})

	wantOps := []string{"pull", "run", "health", "logs", "remove"}
	if !reflect.DeepEqual(runtime.ops, wantOps) {
		t.Fatalf("ops = %v, want %v", runtime.ops, wantOps)
	}
	if runtime.replaceKeep != "" {
		t.Fatalf("replace should not run on failed health check; kept %q", runtime.replaceKeep)
	}
	if !reflect.DeepEqual(runtime.removed, []string{"new-container"}) {
		t.Fatalf("removed = %v, want failed new container only", runtime.removed)
	}
	last := deploy.reports[len(deploy.reports)-1]
	if last.GetStatus() != statusFailed || !strings.Contains(last.GetMessage(), "health check failed") {
		t.Fatalf("last report = status %q message %q, want failed health report", last.GetStatus(), last.GetMessage())
	}
}

func TestExecuteDeployment_RetirePreviousFailureKeepsHealthyReplacement(t *testing.T) {
	oldHealthCheck := runHealthCheck
	defer func() { runHealthCheck = oldHealthCheck }()

	runtime := &fakeRuntime{containerID: "new-container", hostPort: 32768, replaceErr: errors.New("remove old failed")}
	runHealthCheck = func(_ context.Context, _ int32) error {
		runtime.ops = append(runtime.ops, "health")
		return nil
	}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork:       true,
		DeploymentId:  "dep-1",
		ImageRef:      "traefik/whoami:latest",
		ContainerPort: 80,
		AppLabel:      "env-1",
	})

	wantOps := []string{"pull", "run", "health", "replace", "logs"}
	if !reflect.DeepEqual(runtime.ops, wantOps) {
		t.Fatalf("ops = %v, want %v", runtime.ops, wantOps)
	}
	if len(runtime.removed) != 0 {
		t.Fatalf("removed = %v, want no cleanup of healthy replacement", runtime.removed)
	}
	last := deploy.reports[len(deploy.reports)-1]
	if last.GetStatus() != statusRunning || !strings.Contains(last.GetMessage(), "could not remove previous container") {
		t.Fatalf("last report = status %q message %q, want running with cleanup warning", last.GetStatus(), last.GetMessage())
	}
}
