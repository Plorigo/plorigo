//go:build e2e

// End-to-end test for the one-line agent installation flow. It runs the REAL installer
// script and a prebuilt agent binary in a clean ubuntu container against an in-process
// control plane, and proves the agent both REGISTERS and, after a restart, RESUMES its
// persisted identity (no re-registration).
//
// It is not part of `make test` or CI — run it with `make e2e-agent`, which builds a
// linux agent binary and supplies Docker + a migrated Postgres. The harness provides the
// binary (so the container needs no Go); making `curl | sh` self-sufficient on a bare VPS
// (Docker/Caddy prep, idempotency, fresh-Ubuntu preparation) is a later step — see
// ROADMAP.md and docs/architecture/agent.md.
package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/plorigo/plorigo/internal/agents"
	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/platform/config"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/servers"
)

const (
	e2eImage      = "ubuntu:24.04"
	e2eOnlineWait = 120 * time.Second // first run also apt-installs curl + may pull the image
)

func TestE2EAgentInstall(t *testing.T) {
	// Prerequisites — skip clearly when not run via `make e2e-agent`.
	if os.Getenv("DATABASE_URL") == "" || os.Getenv("APP_MASTER_KEY") == "" {
		t.Skip("e2e: set DATABASE_URL and APP_MASTER_KEY (Postgres up + migrated); run `make e2e-agent`")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("e2e: docker not found on PATH; run `make e2e-agent`")
	}
	agentBin := os.Getenv("PLORIGO_E2E_AGENT_BIN")
	if agentBin == "" {
		agentBin = filepath.Join(repoRoot(), "dist", "plorigo-agent")
	}
	if fi, err := os.Stat(agentBin); err != nil || fi.IsDir() {
		t.Skipf("e2e: agent binary %q missing; run `make e2e-agent` (builds it for linux)", agentBin)
	}
	scriptsDir := filepath.Join(repoRoot(), "scripts")

	ctx := context.Background()

	// 1. Boot the control plane and serve it on a container-reachable port (0.0.0.0 so the
	//    agent in a container can reach it via host.docker.internal).
	a, err := New(ctx, config.Load())
	if err != nil {
		t.Fatalf("app.New (is Postgres up and migrated?): %v", err)
	}
	t.Cleanup(func() { a.Close() })

	ln, err := net.Listen("tcp", "0.0.0.0:0")
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
	port := ln.Addr().(*net.TCPAddr).Port
	cpURL := fmt.Sprintf("http://host.docker.internal:%d", port)

	// 2. Fixtures: an owner with a workspace and a server, plus a one-time token — the same
	//    service calls the dashboard's CreateRegistrationToken RPC makes.
	authSvc := a.auth.Service()
	email := "e2e-" + id.New().String()[:8] + "@example.com"
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
	srvRec, err := a.servers.Service().Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "E2E Edge"})
	if err != nil {
		t.Fatalf("Create server: %v", err)
	}
	tok, err := a.agents.Service().CreateRegistrationToken(ownerCtx, srvRec.ID)
	if err != nil {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}

	// 3. A docker volume holds the agent's data dir so the second run resumes from the
	//    identity the first run persisted.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	dataVol := "plorigo-e2e-data-" + suffix
	mustDocker(t, "volume", "create", dataVol)
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", dataVol).Run() })

	// 4. First run: the REAL installer fetches the (harness-provided) binary and starts the
	//    agent, which registers with the one-time token.
	c1 := "plorigo-e2e-install-" + suffix
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", c1).Run() })
	install := fmt.Sprintf(
		"apt-get update -qq && apt-get install -y -qq curl ca-certificates >/dev/null && "+
			"exec sh /mnt/scripts/install-agent.sh --control-plane %s --token %s --binary-url file:///mnt/plorigo-agent --data-dir /data",
		cpURL, tok.Raw,
	)
	mustDocker(t, "run", "-d", "--name", c1,
		"--add-host", "host.docker.internal:host-gateway",
		"-v", scriptsDir+":/mnt/scripts:ro",
		"-v", agentBin+":/mnt/plorigo-agent:ro",
		"-v", dataVol+":/data",
		e2eImage, "sh", "-c", install,
	)

	online := waitOnline(t, a.agents.Service(), ownerCtx, ws.ID, srvRec.ID, e2eOnlineWait, c1)
	agentID := online.ID
	t.Logf("agent registered and online: %s", agentID)

	// 5. Restart: stop the agent, then run the binary again with the SAME data volume and NO
	//    token. It must resume the persisted identity rather than re-register.
	mustDocker(t, "stop", "-t", "5", c1)
	c2 := "plorigo-e2e-resume-" + suffix
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", c2).Run() })
	mustDocker(t, "run", "-d", "--name", c2,
		"--add-host", "host.docker.internal:host-gateway",
		"-v", agentBin+":/mnt/plorigo-agent:ro",
		"-v", dataVol+":/data",
		e2eImage, "/mnt/plorigo-agent", "--control-plane", cpURL, "--data-dir", "/data",
	)

	resumed := waitOnline(t, a.agents.Service(), ownerCtx, ws.ID, srvRec.ID, 60*time.Second, c2)
	if resumed.ID != agentID {
		t.Fatalf("after restart agent id = %s, want the same identity %s (a new id means it re-registered)", resumed.ID, agentID)
	}

	logs := dockerOutput(t, "logs", c2)
	if !strings.Contains(logs, "resuming as agent "+agentID) {
		t.Fatalf("resume run did not log resuming as the persisted identity; logs:\n%s", logs)
	}
	if strings.Contains(logs, "registered as agent") {
		t.Fatalf("resume run re-registered instead of resuming; logs:\n%s", logs)
	}

	// Exactly one agent for the server throughout: the restart reused the identity.
	all, err := a.agents.Service().ListByWorkspace(ownerCtx, ws.ID)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	var n int
	for _, ag := range all {
		if ag.ServerID == srvRec.ID {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("agents for server = %d, want exactly 1 (the install + restart share one identity)", n)
	}
}

// repoRoot resolves the repository root from this test file's location.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// waitOnline polls the dashboard-facing agent view until the server's agent reports
// online, or fails with the container's logs for diagnosis.
func waitOnline(t *testing.T, svc agents.Service, ownerCtx context.Context, wsID, serverID string, timeout time.Duration, container string) agents.Agent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		list, err := svc.ListByWorkspace(ownerCtx, wsID)
		if err != nil {
			t.Fatalf("ListByWorkspace: %v", err)
		}
		for _, ag := range list {
			if ag.ServerID == serverID && ag.Status(time.Now()) == agents.StatusOnline {
				return ag
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent for server %s never came online within %s; %q logs:\n%s",
				serverID, timeout, container, dockerOutput(t, "logs", container))
		}
		time.Sleep(2 * time.Second)
	}
}

func mustDocker(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func dockerOutput(t *testing.T, args ...string) string {
	t.Helper()
	out, _ := exec.Command("docker", args...).CombinedOutput()
	return string(out)
}
