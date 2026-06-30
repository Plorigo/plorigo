//go:build e2e

// End-to-end test for preview teardown (PLO-99). It runs the REAL agent binary as a host
// subprocess (with real Docker + Caddy) against an in-process control plane, and proves the full
// path: deploy a production release of a public git service, create a branch PREVIEW alongside it,
// then TEAR DOWN the preview — the agent stops + removes the preview's container and reconciles
// Caddy so its route drops — and assert the preview is gone, its deployment row is 'torndown', and
// PRODUCTION is untouched (still running, still served through Caddy).
//
// Not part of `make test` or CI — run it with `make e2e-teardown`, which builds a native agent
// binary and supplies Docker, Caddy, and a migrated Postgres. The agent runs on the host so it uses
// the host's Docker daemon plus the `docker` and `caddy` CLIs directly. It clones over the network,
// so it needs internet access.
package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
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

func TestE2EPreviewTeardown(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" || os.Getenv("APP_MASTER_KEY") == "" {
		t.Skip("e2e: set DATABASE_URL and APP_MASTER_KEY (Postgres up + migrated); run `make e2e-teardown`")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("e2e: docker not found on PATH; run `make e2e-teardown`")
	}
	caddyBin, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("e2e: caddy not found on PATH; run `make e2e-teardown` on a host with Caddy installed")
	}
	agentBin := os.Getenv("PLORIGO_E2E_AGENT_BIN")
	if fi, err := os.Stat(agentBin); err != nil || fi.IsDir() {
		t.Skipf("e2e: native agent binary %q missing; run `make e2e-teardown`", agentBin)
	}

	ctx := context.Background()

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
	email := "e2e-teardown-" + id.New().String()[:8] + "@example.com"
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
	proj, err := a.projects.Service().Create(ownerCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "Teardown App"})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	env, err := a.environments.Service().Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Prod"})
	if err != nil {
		t.Fatalf("Create environment: %v", err)
	}
	srvRec, err := a.servers.Service().Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "E2E Teardown Edge"})
	if err != nil {
		t.Fatalf("Create server: %v", err)
	}
	tok, err := a.agents.Service().CreateRegistrationToken(ownerCtx, srvRec.ID)
	if err != nil {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}

	bOwner, bRepo, bBranch := e2eBuildRepo()
	svcID := insertGitService(t, ctx, a, env.ID, proj.ID, ws.ID, bOwner, bRepo, bBranch)

	// Start the real agent on the host: its deploy loop builds/runs deployments; its teardown loop
	// removes previews.
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

	// --- Production release, so we can prove teardown leaves it alone. ---
	prodDep, err := a.deployments.Service().CreateForService(ownerCtx, deployments.CreateForServiceInput{
		ServiceID: svcID, ServerID: srvRec.ID,
	})
	if err != nil {
		t.Fatalf("CreateForService (production): %v", err)
	}
	prod := waitForTerminal(t, ownerCtx, a.deployments.Service(), prodDep.ID, 5*time.Minute, &agentOut)
	if prod.Status != deployments.StatusRunning {
		t.Fatalf("production deployment status = %q, want running; message=%q\nagent output:\n%s", prod.Status, prod.Message, agentOut.String())
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "plorigo-"+prod.ID[:12]).Run() })
	assertServesThroughCaddy(t, caddyHTTPPort, svcID) // production routes at {service-id}.localhost

	// --- Preview of a branch, running ALONGSIDE production on its own route_key. ---
	previewDep, err := a.deployments.Service().CreatePreview(ownerCtx, deployments.CreatePreviewInput{
		ServiceID: svcID, ServerID: srvRec.ID, Branch: bBranch,
	})
	if err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}
	preview := waitForTerminal(t, ownerCtx, a.deployments.Service(), previewDep.ID, 5*time.Minute, &agentOut)
	if preview.Status != deployments.StatusRunning {
		t.Fatalf("preview deployment status = %q, want running; message=%q\nagent output:\n%s", preview.Status, preview.Message, agentOut.String())
	}
	if preview.Kind != deployments.KindPreview || preview.RouteKey == "" {
		t.Fatalf("preview = %+v, want kind=preview with a route_key", preview)
	}
	previewContainer := "plorigo-" + preview.ID[:12]
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", previewContainer).Run() })
	assertServesThroughCaddy(t, caddyHTTPPort, preview.RouteKey) // preview routes at {route_key}.localhost

	// --- Tear the preview down. ---
	job, err := a.deployments.Service().TeardownPreview(ownerCtx, preview.ID)
	if err != nil {
		t.Fatalf("TeardownPreview: %v", err)
	}
	done := waitForTeardown(t, ownerCtx, a.deployments.Service(), svcID, job.ID, 2*time.Minute, &agentOut)
	if done.Status != deployments.TeardownStatusSucceeded {
		t.Fatalf("teardown status = %q, want succeeded; error=%q\nagent output:\n%s", done.Status, done.Error, agentOut.String())
	}

	// The preview's container is gone and its deployment row is terminal 'torndown'.
	assertContainerGone(t, previewContainer, 60*time.Second)
	tornDown, err := a.deployments.Service().Get(ownerCtx, preview.ID)
	if err != nil {
		t.Fatalf("Get preview after teardown: %v", err)
	}
	if tornDown.Status != deployments.StatusTornDown {
		t.Fatalf("preview status after teardown = %q, want torndown", tornDown.Status)
	}

	// PRODUCTION is untouched: its container still runs and it still serves through Caddy.
	stillProd, err := a.deployments.Service().Get(ownerCtx, prodDep.ID)
	if err != nil {
		t.Fatalf("Get production after teardown: %v", err)
	}
	if stillProd.Status != deployments.StatusRunning {
		t.Fatalf("production status after preview teardown = %q, want running (teardown must not touch production)", stillProd.Status)
	}
	assertServesThroughCaddy(t, caddyHTTPPort, svcID)
	t.Logf("preview teardown OK: %s removed, production %s still serving", preview.RouteKey, svcID)
}

// waitForTeardown polls a service's teardown jobs until the given one is terminal (succeeded or
// failed) or the timeout elapses, dumping the agent output on timeout.
func waitForTeardown(t *testing.T, ctx context.Context, svc deployments.Service, serviceID, teardownID string, timeout time.Duration, agentOut *strings.Builder) deployments.TeardownJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		rows, err := svc.ListTeardownsByService(ctx, serviceID)
		if err != nil {
			t.Fatalf("ListTeardownsByService: %v", err)
		}
		for _, r := range rows {
			if r.ID != teardownID {
				continue
			}
			if r.Status == deployments.TeardownStatusSucceeded || r.Status == deployments.TeardownStatusFailed {
				return r
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("teardown %s did not finish within %s\nagent output:\n%s", teardownID, timeout, agentOut.String())
		}
		time.Sleep(time.Second)
	}
}

// assertContainerGone waits until `docker inspect` no longer finds the container (teardown is
// asynchronous: the agent removes it shortly after reporting succeeded).
func assertContainerGone(t *testing.T, container string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if err := exec.Command("docker", "inspect", container).Run(); err != nil {
			return // non-zero exit => the container no longer exists
		}
		if time.Now().After(deadline) {
			t.Fatalf("preview container %s still present %s after teardown", container, timeout)
		}
		time.Sleep(time.Second)
	}
}
