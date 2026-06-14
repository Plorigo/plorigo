package agentcore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	go func() { done <- heartbeatLoop(ctx, &out, client, ident, nil, opts) }()

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
	if err := heartbeatLoop(ctx, io.Discard, client, ident, nil, Options{HeartbeatInterval: time.Millisecond}); err != nil {
		t.Fatalf("heartbeatLoop returned error: %v", err)
	}
	if calls := client.registerCalls.Load(); calls != 0 {
		t.Errorf("register calls = %d, want 0 without a token", calls)
	}
	if got := ident.get(); got.Credential != "plag_stale" {
		t.Errorf("identity = %+v, want unchanged", got)
	}
}

// recordingClient accepts any heartbeat and hands the request to a test so it can assert
// what the agent put on the wire.
type recordingClient struct {
	got chan *agentv1.HeartbeatRequest
}

func (c *recordingClient) Register(_ context.Context, _ *connect.Request[agentv1.RegisterRequest]) (*connect.Response[agentv1.RegisterResponse], error) {
	return nil, errors.New("register is not exercised by this test")
}

func (c *recordingClient) Heartbeat(_ context.Context, req *connect.Request[agentv1.HeartbeatRequest]) (*connect.Response[agentv1.HeartbeatResponse], error) {
	select {
	case c.got <- req.Msg:
	default:
	}
	return connect.NewResponse(&agentv1.HeartbeatResponse{}), nil
}

func TestHeartbeatLoop_ReportsHealthFacts(t *testing.T) {
	client := &recordingClient{got: make(chan *agentv1.HeartbeatRequest, 1)}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- heartbeatLoop(ctx, io.Discard, client, ident, stubProber{version: "27.1.1"}, Options{HeartbeatInterval: time.Millisecond})
	}()

	select {
	case msg := <-client.got:
		if !msg.GetDockerAvailable() || msg.GetDockerVersion() != "27.1.1" {
			t.Errorf("docker facts = (%v, %q), want (true, 27.1.1)", msg.GetDockerAvailable(), msg.GetDockerVersion())
		}
		if msg.GetOs() != runtime.GOOS || msg.GetArch() != runtime.GOARCH {
			t.Errorf("os/arch = %q/%q, want %q/%q", msg.GetOs(), msg.GetArch(), runtime.GOOS, runtime.GOARCH)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("no heartbeat observed within the timeout")
	}
	cancel()
	<-done
}

// --- identity persistence & resume ------------------------------------------
//
// AC#2 of the install flow is "register AND resume using the installed identity".
// These cover the resume contract deterministically (no Docker, no network); the
// full online -> restart -> resume path on a real server is covered by the Docker
// end-to-end harness (make e2e-agent).

func TestStatePersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := &state{AgentID: "agent-7", Credential: "plag_durable", PrivateKeyB64: "a2V5"}
	if err := saveState(dir, want); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	got, err := loadState(dir)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if got == nil || *got != *want {
		t.Fatalf("loadState = %+v, want %+v", got, want)
	}
	// The credential and private key are secret, so the file is written 0600.
	fi, err := os.Stat(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("state file mode = %o, want 600", perm)
	}
}

func TestLoadStateMissingOrIncompleteTriggersRegister(t *testing.T) {
	// No file yet: there is no identity, so Run() registers rather than resumes.
	if st, err := loadState(t.TempDir()); err != nil || st != nil {
		t.Fatalf("loadState(missing) = %+v, %v; want nil, nil", st, err)
	}
	// A half-written file (id but no credential) is treated as no identity, so the
	// agent re-registers instead of resuming with an unusable credential.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte(`{"agent_id":"agent-7"}`), 0o600); err != nil {
		t.Fatalf("write incomplete state: %v", err)
	}
	if st, err := loadState(dir); err != nil || st != nil {
		t.Fatalf("loadState(incomplete) = %+v, %v; want nil, nil", st, err)
	}
}

func TestRunWithoutStateOrTokenErrors(t *testing.T) {
	// No persisted identity and no registration token: the agent cannot register and
	// says so clearly (before any network or Docker work) instead of hanging.
	err := Run(context.Background(), io.Discard, Options{
		ControlPlaneURL: "http://127.0.0.1:1",
		DataDir:         t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "no registration token") {
		t.Fatalf("Run err = %v, want a no-registration-token error", err)
	}
}

func TestRunResumesFromPersistedIdentity(t *testing.T) {
	// With a persisted identity, Run() resumes as that agent — it does NOT re-register —
	// even before the control plane is reachable. We assert the resume log line here; the
	// online + same-identity path is proven against a real server by make e2e-agent.
	dir := t.TempDir()
	if err := saveState(dir, &state{AgentID: "agent-resume", Credential: "plag_durable", PrivateKeyB64: "a2V5"}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var out strings.Builder
	// 127.0.0.1:1 refuses fast, so the heartbeat/deploy loops just back off until ctx ends.
	_ = Run(ctx, &out, Options{
		ControlPlaneURL:   "http://127.0.0.1:1",
		DataDir:           dir,
		HeartbeatInterval: time.Millisecond,
		PollInterval:      time.Millisecond,
	})
	if !strings.Contains(out.String(), "resuming as agent agent-resume") {
		t.Fatalf("output = %q, want a resume line (no re-registration)", out.String())
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

	cloneSHA     string
	cloneErr     error
	buildErr     error
	builtTag     string // the tag build() was asked to produce
	ranImage     string // the image run() was asked to start
	ranPort      int32  // the container port run() was asked to publish
	detectedPort int32  // the port detectPort() returns (0 -> default 8080)
	detectErr    error
}

func (f *fakeRuntime) pull(_ context.Context, _ string, emit func(string)) error {
	f.ops = append(f.ops, "pull")
	emit("pull complete")
	return nil
}

func (f *fakeRuntime) clone(_ context.Context, _, _, _ string, emit func(string)) (string, error) {
	f.ops = append(f.ops, "clone")
	emit("cloned")
	if f.cloneErr != nil {
		return "", f.cloneErr
	}
	if f.cloneSHA == "" {
		return "deadbeefcafef00d", nil
	}
	return f.cloneSHA, nil
}

func (f *fakeRuntime) build(_ context.Context, _, tag string, emit func(string)) error {
	f.ops = append(f.ops, "build")
	f.builtTag = tag
	emit("built")
	return f.buildErr
}

func (f *fakeRuntime) detectPort(_ context.Context, _ string) (int32, error) {
	f.ops = append(f.ops, "detect")
	if f.detectErr != nil {
		return 0, f.detectErr
	}
	if f.detectedPort == 0 {
		return 8080, nil
	}
	return f.detectedPort, nil
}

func (f *fakeRuntime) run(_ context.Context, in runInput) (string, int32, error) {
	f.ops = append(f.ops, "run")
	f.ranImage = in.imageRef
	f.ranPort = in.containerPort
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

func TestExecuteDeployment_GitClonesAndBuildsThenRunsBuiltImage(t *testing.T) {
	oldHealthCheck := runHealthCheck
	defer func() { runHealthCheck = oldHealthCheck }()

	runtime := &fakeRuntime{containerID: "new-container", hostPort: 32768, cloneSHA: "commit-sha-1"}
	runHealthCheck = func(_ context.Context, _ int32) error {
		runtime.ops = append(runtime.ops, "health")
		return nil
	}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork:       true,
		DeploymentId:  "dep-1",
		ContainerPort: 80,
		AppLabel:      "env-1",
		SourceKind:    "git",
		CloneUrl:      "https://github.com/o/r.git",
		GitRef:        "main",
		BuiltImageTag: "plorigo-build:dep-1",
	})

	// Git path clones and builds first, then runs the built image — and never pulls.
	wantOps := []string{"clone", "build", "run", "health", "replace", "logs"}
	if !reflect.DeepEqual(runtime.ops, wantOps) {
		t.Fatalf("ops = %v, want %v (clone+build, no pull)", runtime.ops, wantOps)
	}
	if runtime.builtTag != "plorigo-build:dep-1" || runtime.ranImage != "plorigo-build:dep-1" {
		t.Fatalf("built/ran image = %q/%q, want the built tag", runtime.builtTag, runtime.ranImage)
	}
	last := deploy.reports[len(deploy.reports)-1]
	if last.GetStatus() != statusRunning || last.GetCommitSha() != "commit-sha-1" || last.GetBuiltImageRef() != "plorigo-build:dep-1" {
		t.Fatalf("last report = %+v, want running with commit + built image", last)
	}
}

func TestExecuteDeployment_GitCloneFailureReportsFailedBeforeBuild(t *testing.T) {
	runtime := &fakeRuntime{cloneErr: errors.New("repo not found")}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork: true, DeploymentId: "dep-1", ContainerPort: 80, AppLabel: "env-1",
		SourceKind: "git", CloneUrl: "https://github.com/o/missing.git", GitRef: "main", BuiltImageTag: "plorigo-build:dep-1",
	})

	if !reflect.DeepEqual(runtime.ops, []string{"clone"}) {
		t.Fatalf("ops = %v, want clone only (no build/run after a clone failure)", runtime.ops)
	}
	last := deploy.reports[len(deploy.reports)-1]
	if last.GetStatus() != statusFailed || !strings.Contains(last.GetMessage(), "clone failed") {
		t.Fatalf("last report = status %q message %q, want a clone-failed report", last.GetStatus(), last.GetMessage())
	}
}

func TestExecuteDeployment_GitBuildFailureReportsFailedBeforeRun(t *testing.T) {
	runtime := &fakeRuntime{buildErr: errors.New("no Dockerfile at the repository root")}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork: true, DeploymentId: "dep-1", ContainerPort: 80, AppLabel: "env-1",
		SourceKind: "git", CloneUrl: "https://github.com/o/r.git", GitRef: "main", BuiltImageTag: "plorigo-build:dep-1",
	})

	if !reflect.DeepEqual(runtime.ops, []string{"clone", "build"}) {
		t.Fatalf("ops = %v, want clone+build only (no run after a build failure)", runtime.ops)
	}
	last := deploy.reports[len(deploy.reports)-1]
	if last.GetStatus() != statusFailed || !strings.Contains(last.GetMessage(), "build failed") {
		t.Fatalf("last report = status %q message %q, want a build-failed report", last.GetStatus(), last.GetMessage())
	}
}

func TestExecuteDeployment_GitAutoDetectsPortWhenUnset(t *testing.T) {
	oldHealthCheck := runHealthCheck
	defer func() { runHealthCheck = oldHealthCheck }()

	runtime := &fakeRuntime{containerID: "new-container", hostPort: 32768, detectedPort: 3000}
	runHealthCheck = func(_ context.Context, _ int32) error {
		runtime.ops = append(runtime.ops, "health")
		return nil
	}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork: true, DeploymentId: "dep-1", ContainerPort: 0, AppLabel: "env-1",
		SourceKind: "git", CloneUrl: "https://github.com/o/r.git", GitRef: "main", BuiltImageTag: "plorigo-build:dep-1",
	})

	// With no port set, the agent detects it from the built image before running.
	wantOps := []string{"clone", "build", "detect", "run", "health", "replace", "logs"}
	if !reflect.DeepEqual(runtime.ops, wantOps) {
		t.Fatalf("ops = %v, want %v (detect between build and run)", runtime.ops, wantOps)
	}
	if runtime.ranPort != 3000 {
		t.Fatalf("ran port = %d, want the detected 3000", runtime.ranPort)
	}
}

func TestExecuteDeployment_GitExplicitPortSkipsDetect(t *testing.T) {
	oldHealthCheck := runHealthCheck
	defer func() { runHealthCheck = oldHealthCheck }()

	runtime := &fakeRuntime{containerID: "new-container", hostPort: 32768, detectedPort: 3000}
	runHealthCheck = func(_ context.Context, _ int32) error {
		runtime.ops = append(runtime.ops, "health")
		return nil
	}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork: true, DeploymentId: "dep-1", ContainerPort: 5000, AppLabel: "env-1",
		SourceKind: "git", CloneUrl: "https://github.com/o/r.git", GitRef: "main", BuiltImageTag: "plorigo-build:dep-1",
	})

	// An explicit port is honored as-is — no detection.
	wantOps := []string{"clone", "build", "run", "health", "replace", "logs"}
	if !reflect.DeepEqual(runtime.ops, wantOps) {
		t.Fatalf("ops = %v, want %v (no detect when a port is set)", runtime.ops, wantOps)
	}
	if runtime.ranPort != 5000 {
		t.Fatalf("ran port = %d, want the explicit 5000", runtime.ranPort)
	}
}

func TestExecuteDeployment_GitPortDetectFailureReportsFailed(t *testing.T) {
	runtime := &fakeRuntime{containerID: "new-container", hostPort: 32768, detectErr: errors.New("the image exposes no TCP port")}
	deploy := &fakeDeployClient{}
	ident := &identity{st: state{AgentID: "agent-1", Credential: "plag_1"}}

	executeDeployment(context.Background(), io.Discard, deploy, ident, runtime, &agentv1.PollDeploymentResponse{
		HasWork: true, DeploymentId: "dep-1", ContainerPort: 0, AppLabel: "env-1",
		SourceKind: "git", CloneUrl: "https://github.com/o/r.git", GitRef: "main", BuiltImageTag: "plorigo-build:dep-1",
	})

	if !reflect.DeepEqual(runtime.ops, []string{"clone", "build", "detect"}) {
		t.Fatalf("ops = %v, want clone+build+detect only (no run when no port can be determined)", runtime.ops)
	}
	last := deploy.reports[len(deploy.reports)-1]
	if last.GetStatus() != statusFailed || !strings.Contains(last.GetMessage(), "port") {
		t.Fatalf("last report = status %q message %q, want a port-detection failure", last.GetStatus(), last.GetMessage())
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
