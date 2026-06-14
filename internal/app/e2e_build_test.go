//go:build e2e

// End-to-end test for build-and-deploy from a Git source (PLO-12). It runs the REAL agent
// binary as a host subprocess (with real Docker) against an in-process control plane, and
// proves the full path: the agent claims a git deployment, CLONES a public repo, BUILDS its
// Dockerfile with BuildKit, RUNS the image, routes it through Caddy, and reports it running.
// A second deployment of a repo with no Dockerfile must surface as a clear build failure.
//
// Not part of `make test` or CI — run it with `make e2e-build`, which builds a native agent
// binary and supplies Docker, Caddy, and a migrated Postgres. The agent runs on the host (not
// in a container) so it uses the host's Docker daemon plus the `docker` and `caddy` CLIs
// directly. It clones over the network, so it needs internet access.
package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/deployments"
	"github.com/plorigo/plorigo/internal/environments"
	"github.com/plorigo/plorigo/internal/platform/config"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/projects"
	"github.com/plorigo/plorigo/internal/servers"
)

// The public repo to build. Overridable so the runner isn't pinned to one upstream. The repo
// must have a Dockerfile at its root with an `EXPOSE` (so the port auto-detects) that serves
// HTTP. docker/welcome-to-docker EXPOSEs 3000 and serves there.
func e2eBuildRepo() (owner, repo, branch string) {
	return envOr("PLORIGO_E2E_BUILD_OWNER", "docker"),
		envOr("PLORIGO_E2E_BUILD_REPO", "welcome-to-docker"),
		envOr("PLORIGO_E2E_BUILD_BRANCH", "main")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func TestE2EBuildDeploy(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" || os.Getenv("APP_MASTER_KEY") == "" {
		t.Skip("e2e: set DATABASE_URL and APP_MASTER_KEY (Postgres up + migrated); run `make e2e-build`")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("e2e: docker not found on PATH; run `make e2e-build`")
	}
	caddyBin, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("e2e: caddy not found on PATH; run `make e2e-build` on a host with Caddy installed")
	}
	agentBin := os.Getenv("PLORIGO_E2E_AGENT_BIN")
	if fi, err := os.Stat(agentBin); err != nil || fi.IsDir() {
		t.Skipf("e2e: native agent binary %q missing; run `make e2e-build`", agentBin)
	}

	ctx := context.Background()

	// Boot the control plane on a loopback port the host agent can reach.
	a, err := New(ctx, config.Load())
	if err != nil {
		t.Fatalf("app.New (is Postgres up and migrated?): %v", err)
	}
	t.Cleanup(func() { a.Close() })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	srv := &http.Server{Handler: a.router(), ReadHeaderTimeout: 10 * time.Second, Protocols: protocols}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	})
	cpURL := fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port)
	caddyConfig, caddyHTTPPort, caddyAdmin := startE2ECaddy(t, caddyBin)

	// Fixtures: owner + workspace + project + environment + server + registration token.
	authSvc := a.auth.Service()
	email := "e2e-build-" + id.New().String()[:8] + "@example.com"
	if _, err := authSvc.Register(ctx, auth.RegisterInput{Email: email, Password: "supersecret"}); err != nil {
		t.Fatalf("Register owner: %v", err)
	}
	owner, err := authSvc.Login(ctx, auth.LoginInput{Email: email, Password: "supersecret"})
	if err != nil {
		t.Fatalf("Login owner: %v", err)
	}
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})
	wss, err := a.projects.Service().ListMyWorkspaces(ctx, owner.User.ID)
	if err != nil || len(wss) != 1 {
		t.Fatalf("ListMyWorkspaces: wss=%d err=%v", len(wss), err)
	}
	ws := wss[0]
	proj, err := a.projects.Service().Create(ownerCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "Build App"})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	env, err := a.environments.Service().Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Prod"})
	if err != nil {
		t.Fatalf("Create environment: %v", err)
	}
	srvRec, err := a.servers.Service().Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "E2E Build Edge"})
	if err != nil {
		t.Fatalf("Create server: %v", err)
	}
	tok, err := a.agents.Service().CreateRegistrationToken(ownerCtx, srvRec.ID)
	if err != nil {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}

	bOwner, bRepo, bBranch := e2eBuildRepo()
	insertPublicSource(t, ctx, a, proj.ID, bOwner, bRepo, bBranch)

	// Start the real agent on the host. It registers, then its deploy loop builds whatever we
	// queue. Capture its output for diagnosis.
	var agentOut strings.Builder
	actx, acancel := context.WithCancel(ctx)
	defer acancel()
	cmd := exec.CommandContext(actx, agentBin,
		"--control-plane", cpURL, "--token", tok.Raw, "--data-dir", t.TempDir(),
		"--caddy-bin", caddyBin,
		"--caddy-config", caddyConfig,
		"--caddy-base-domain", "localhost",
		"--caddy-http-port", strconv.Itoa(caddyHTTPPort),
		"--caddy-admin", caddyAdmin)
	cmd.Stdout = &agentOut
	cmd.Stderr = &agentOut
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agent: %v", err)
	}
	t.Cleanup(func() { acancel(); _ = cmd.Wait() })

	// --- Success: a public repo with a Dockerfile builds and runs, with the port AUTO-DETECTED
	// from the image's EXPOSE (no container port given). ---
	dep, err := a.deployments.Service().CreateFromSource(ownerCtx, deployments.CreateFromSourceInput{
		EnvironmentID: env.ID, ServerID: srvRec.ID, ContainerPort: 0,
	})
	if err != nil {
		t.Fatalf("CreateFromSource: %v", err)
	}
	t.Logf("queued git deployment %s for %s/%s@%s (auto-detect port)", dep.ID, bOwner, bRepo, bBranch)

	got := waitForTerminal(t, ownerCtx, a.deployments.Service(), dep.ID, 5*time.Minute, &agentOut)
	if got.Status != deployments.StatusRunning {
		t.Fatalf("git deployment status = %q, want running; message=%q\nagent output:\n%s", got.Status, got.Message, agentOut.String())
	}
	if !strings.Contains(agentOut.String(), "auto-detected container port") {
		t.Fatalf("expected the agent to auto-detect the port from EXPOSE; agent output:\n%s", agentOut.String())
	}
	if got.CommitSha == "" || got.BuiltImageRef == "" {
		t.Fatalf("running git deployment missing commit/built image: %+v", got)
	}
	if got.HostPort <= 0 {
		t.Fatalf("running git deployment has no host port: %+v", got)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "plorigo-"+got.ID[:12]).Run() })

	// The built app actually serves through the Caddy route. The host port remains
	// bound, but it is now the internal Caddy upstream target.
	assertServesThroughCaddy(t, caddyHTTPPort, env.ID)
	assertServes(t, got.HostPort)

	// The timeline went through the build phases.
	assertReachedStatuses(t, ownerCtx, a.deployments.Service(), dep.ID, deployments.StatusCloning, deployments.StatusBuilding, deployments.StatusRouting, deployments.StatusRunning)

	// Logs are attributed to the right stream: the clone/build output is build, the
	// container's own output is runtime (PLO-13).
	assertHasStreamLogs(t, ownerCtx, a.deployments.Service(), dep.ID)

	// Runtime logs keep flowing AFTER the deploy: the agent's tail loop streams the running
	// container's output, not just the snapshot taken at startup. Drive a little traffic,
	// then assert a runtime log lands beyond that snapshot.
	assertRuntimeLogsAccumulate(t, ownerCtx, a.deployments.Service(), dep.ID, got.HostPort)

	// --- Failure: a repo with no Dockerfile fails with a clear message. ---
	noDockerProj, _ := a.projects.Service().Create(ownerCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "No Dockerfile"})
	noDockerEnv, _ := a.environments.Service().Create(ownerCtx, environments.CreateInput{ProjectID: noDockerProj.ID, Name: "Prod"})
	insertPublicSource(t, ctx, a, noDockerProj.ID,
		envOr("PLORIGO_E2E_NODOCKER_OWNER", "octocat"),
		envOr("PLORIGO_E2E_NODOCKER_REPO", "Hello-World"),
		envOr("PLORIGO_E2E_NODOCKER_BRANCH", "master"))
	failDep, err := a.deployments.Service().CreateFromSource(ownerCtx, deployments.CreateFromSourceInput{
		EnvironmentID: noDockerEnv.ID, ServerID: srvRec.ID, ContainerPort: 80,
	})
	if err != nil {
		t.Fatalf("CreateFromSource (no Dockerfile): %v", err)
	}
	failed := waitForTerminal(t, ownerCtx, a.deployments.Service(), failDep.ID, 2*time.Minute, &agentOut)
	if failed.Status != deployments.StatusFailed || !strings.Contains(strings.ToLower(failed.Message), "dockerfile") {
		t.Fatalf("no-Dockerfile deployment = status %q message %q, want a failed build mentioning the Dockerfile", failed.Status, failed.Message)
	}
}

func insertPublicSource(t *testing.T, ctx context.Context, a *App, projectID, owner, repo, branch string) {
	t.Helper()
	if _, err := a.db.Pool.Exec(ctx,
		`INSERT INTO project_sources (project_id, owner, repo, full_name, branch, default_branch, access, html_url)
		 VALUES ($1, $2, $3, $4, $5, $5, 'public', $6)
		 ON CONFLICT (project_id) DO UPDATE SET owner=$2, repo=$3, full_name=$4, branch=$5, default_branch=$5, access='public', html_url=$6`,
		projectID, owner, repo, owner+"/"+repo, branch, "https://github.com/"+owner+"/"+repo); err != nil {
		t.Fatalf("insert public source: %v", err)
	}
}

// waitForTerminal polls until the deployment reaches a terminal status (running/failed/
// superseded) or the timeout elapses, dumping the agent output on timeout.
func waitForTerminal(t *testing.T, ctx context.Context, svc deployments.Service, depID string, timeout time.Duration, agentOut *strings.Builder) deployments.Deployment {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		dep, err := svc.Get(ctx, depID)
		if err != nil {
			t.Fatalf("Get deployment: %v", err)
		}
		switch dep.Status {
		case deployments.StatusRunning, deployments.StatusFailed, deployments.StatusSuperseded:
			return dep
		}
		if time.Now().After(deadline) {
			t.Fatalf("deployment %s never finished within %s (last status %q); agent output:\n%s", depID, timeout, dep.Status, agentOut.String())
		}
		time.Sleep(2 * time.Second)
	}
}

func assertReachedStatuses(t *testing.T, ctx context.Context, svc deployments.Service, depID string, want ...string) {
	t.Helper()
	events, err := svc.ListEvents(ctx, depID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range events {
		if e.Kind == deployments.KindStatus {
			seen[e.Status] = true
		}
	}
	for _, w := range want {
		if !seen[w] {
			t.Fatalf("timeline did not reach %q; statuses seen: %v", w, seen)
		}
	}
}

// assertHasStreamLogs checks both log streams are attributed: build output from the
// clone/build phase, and runtime output from the container's deploy-time snapshot.
func assertHasStreamLogs(t *testing.T, ctx context.Context, svc deployments.Service, depID string) {
	t.Helper()
	events, err := svc.ListEvents(ctx, depID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var build, runtime int
	for _, e := range events {
		if e.Kind != deployments.KindLog {
			continue
		}
		switch e.Stream {
		case deployments.StreamBuild:
			build++
		case deployments.StreamRuntime:
			runtime++
		}
	}
	if build == 0 {
		t.Fatalf("no build-stream log events; want the clone/build output tagged %q", deployments.StreamBuild)
	}
	if runtime == 0 {
		t.Fatalf("no runtime-stream log events; want the container output tagged %q", deployments.StreamRuntime)
	}
}

// assertRuntimeLogsAccumulate proves the agent keeps tailing the RUNNING container, not just
// the snapshot taken at startup: it records the newest runtime-log seq now, drives some HTTP
// traffic so the app logs, and waits for a runtime-log event beyond that seq to appear.
func assertRuntimeLogsAccumulate(t *testing.T, ctx context.Context, svc deployments.Service, depID string, hostPort int32) {
	t.Helper()
	base := maxRuntimeLogSeq(t, ctx, svc, depID)
	addr := fmt.Sprintf("http://127.0.0.1:%d/", hostPort)
	deadline := time.Now().Add(30 * time.Second)
	for {
		// Generate output — most servers log each request to stdout/stderr.
		if resp, err := http.Get(addr); err == nil { //nolint:gosec // loopback test traffic
			_ = resp.Body.Close()
		}
		if maxRuntimeLogSeq(t, ctx, svc, depID) > base {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no new runtime-stream log after the startup snapshot (seq still <= %d): the tail loop isn't streaming the running container's output. If the test image is too quiet, override PLORIGO_E2E_BUILD_* with one that logs per request.", base)
		}
		time.Sleep(2 * time.Second)
	}
}

func maxRuntimeLogSeq(t *testing.T, ctx context.Context, svc deployments.Service, depID string) int64 {
	t.Helper()
	events, err := svc.ListEvents(ctx, depID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var maxSeq int64
	for _, e := range events {
		if e.Kind == deployments.KindLog && e.Stream == deployments.StreamRuntime && e.Seq > maxSeq {
			maxSeq = e.Seq
		}
	}
	return maxSeq
}

func startE2ECaddy(t *testing.T, caddyBin string) (managedConfig string, httpPort int, adminAddr string) {
	t.Helper()
	httpPort = freeTCPPort(t)
	adminPort := freeTCPPort(t)
	adminAddr = fmt.Sprintf("127.0.0.1:%d", adminPort)
	dir := t.TempDir()
	initialConfig := filepath.Join(dir, "initial.Caddyfile")
	managedConfig = filepath.Join(dir, "managed.Caddyfile")
	initial := fmt.Sprintf(`{
	admin %s
	auto_https off
}

http://localhost:%d {
	respond "plorigo caddy ready"
}
`, adminAddr, httpPort)
	if err := os.WriteFile(initialConfig, []byte(initial), 0o600); err != nil {
		t.Fatalf("write initial Caddyfile: %v", err)
	}
	var out strings.Builder
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, caddyBin, "run", "--config", initialConfig, "--adapter", "caddyfile")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start caddy: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
	waitTCP(t, adminAddr, 15*time.Second, func() string { return out.String() })
	return managedConfig, httpPort, adminAddr
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve tcp port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitTCP(t *testing.T, addr string, timeout time.Duration, logs func() string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tcp %s not reachable within %s: %v\nlogs:\n%s", addr, timeout, err, logs())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func assertServesThroughCaddy(t *testing.T, caddyPort int, environmentID string) {
	t.Helper()
	addr := fmt.Sprintf("http://127.0.0.1:%d/", caddyPort)
	host := environmentID + ".localhost"
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	var lastStatus int
	for {
		req, err := http.NewRequest(http.MethodGet, addr, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Host = host
		resp, err := http.DefaultClient.Do(req) //nolint:gosec // loopback test traffic
		if err == nil {
			lastStatus = resp.StatusCode
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				return
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("built app not reachable through Caddy at %s with Host %s within timeout: status=%d err=%v", addr, host, lastStatus, lastErr)
		}
		time.Sleep(time.Second)
	}
}

func assertServes(t *testing.T, hostPort int32) {
	t.Helper()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(hostPort)))
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("built app not reachable on %s within timeout: %v", addr, err)
		}
		time.Sleep(time.Second)
	}
}
