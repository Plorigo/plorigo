//go:build e2e

// End-to-end test for database backup + restore (PLO-23, PLO-24). It runs the REAL agent binary
// as a host subprocess (with real Docker) against an in-process control plane, and proves the
// full path: provision a managed Postgres service, seed rows, take a backup (the agent runs
// pg_dump inside the container), DROP the data, restore the backup (the agent pipes the dump into
// psql), and assert the rows came back.
//
// Not part of `make test` or CI — run it with `make e2e-backup`, which builds a native agent
// binary and supplies Docker, Caddy, and a migrated Postgres. The agent runs on the host so it
// uses the host's Docker daemon and the `docker` CLI directly.
package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/backups"
	"github.com/plorigo/plorigo/internal/environments"
	"github.com/plorigo/plorigo/internal/platform/config"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/projects"
	"github.com/plorigo/plorigo/internal/servers"
	"github.com/plorigo/plorigo/internal/services"
)

func TestE2EBackupRestore(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" || os.Getenv("APP_MASTER_KEY") == "" {
		t.Skip("e2e: set DATABASE_URL and APP_MASTER_KEY (Postgres up + migrated); run `make e2e-backup`")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("e2e: docker not found on PATH; run `make e2e-backup`")
	}
	caddyBin, err := exec.LookPath("caddy")
	if err != nil {
		t.Skip("e2e: caddy not found on PATH; run `make e2e-backup` on a host with Caddy installed")
	}
	agentBin := os.Getenv("PLORIGO_E2E_AGENT_BIN")
	if fi, err := os.Stat(agentBin); err != nil || fi.IsDir() {
		t.Skipf("e2e: native agent binary %q missing; run `make e2e-backup`", agentBin)
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
	email := "e2e-backup-" + id.New().String()[:8] + "@example.com"
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
	proj, err := a.projects.Service().Create(ownerCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "Backup App"})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	env, err := a.environments.Service().Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Prod"})
	if err != nil {
		t.Fatalf("Create environment: %v", err)
	}
	srvRec, err := a.servers.Service().Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "E2E Backup Edge"})
	if err != nil {
		t.Fatalf("Create server: %v", err)
	}
	tok, err := a.agents.Service().CreateRegistrationToken(ownerCtx, srvRec.ID)
	if err != nil {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}

	// Start the real agent on the host. Its deploy loop runs the database; its backup loop runs
	// backups and restores.
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

	// Provision + deploy a managed Postgres database on the agent.
	dbRes, err := a.services.Service().CreateDatabase(ownerCtx, services.DatabaseInput{
		EnvironmentID: env.ID, Name: "db", TemplateID: "postgres", ServerID: srvRec.ID, DeployNow: true,
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	dep := waitForTerminal(t, ownerCtx, a.deployments.Service(), dbRes.DeploymentID, 3*time.Minute, &agentOut)
	if dep.Status != "running" {
		t.Fatalf("database deployment status = %q, want running; message=%q\nagent output:\n%s", dep.Status, dep.Message, agentOut.String())
	}
	container := "plorigo-" + dep.ID[:12]
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })

	user, password, dbName := parseConnURI(t, dbRes.ConnectionURI)
	// Postgres in the container needs a moment to accept connections after the container is up.
	waitForPostgres(t, container, user, password, dbName, 60*time.Second)

	// Seed deterministic rows.
	psql(t, container, user, password, dbName, "CREATE TABLE widgets (id int primary key); INSERT INTO widgets VALUES (1),(2),(3);")
	if got := psqlCount(t, container, user, password, dbName, "SELECT count(*) FROM widgets"); got != 3 {
		t.Fatalf("seed: widgets count = %d, want 3", got)
	}

	// Back it up and wait for success.
	b, err := a.backups.Service().CreateBackup(ownerCtx, dbRes.Service.ID, "e2e snapshot")
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	done := waitForBackup(t, ownerCtx, a.backups.Service(), b.ID, 2*time.Minute, &agentOut)
	if done.Status != backups.StatusSucceeded {
		t.Fatalf("backup status = %q, want succeeded; error=%q\nagent output:\n%s", done.Status, done.Error, agentOut.String())
	}
	if done.SizeBytes <= 0 || done.Checksum == "" {
		t.Fatalf("succeeded backup missing size/checksum: %+v", done)
	}

	// Destroy the data, then restore the backup and assert the rows came back.
	psql(t, container, user, password, dbName, "DROP TABLE widgets;")
	if got := psqlCount(t, container, user, password, dbName, "SELECT count(*) FROM information_schema.tables WHERE table_name='widgets'"); got != 0 {
		t.Fatalf("after drop: widgets table still present")
	}

	r, err := a.backups.Service().RestoreBackup(ownerCtx, b.ID)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	restored := waitForRestore(t, ownerCtx, a.backups.Service(), dbRes.Service.ID, r.ID, 2*time.Minute, &agentOut)
	if restored.Status != backups.RestoreStatusSucceeded {
		t.Fatalf("restore status = %q, want succeeded; error=%q\nagent output:\n%s", restored.Status, restored.Error, agentOut.String())
	}
	if got := psqlCount(t, container, user, password, dbName, "SELECT count(*) FROM widgets"); got != 3 {
		t.Fatalf("after restore: widgets count = %d, want 3 (restore did not bring the data back)", got)
	}
	t.Logf("backup+restore round-trip OK: %d bytes, checksum %s", done.SizeBytes, done.Checksum[:12])
}

// --- helpers (backup/restore e2e) ---

func parseConnURI(t *testing.T, uri string) (user, password, dbName string) {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse connection uri: %v", err)
	}
	pw, _ := u.User.Password()
	return u.User.Username(), pw, strings.TrimPrefix(u.Path, "/")
}

func dockerExecPsql(container, user, password, dbName string, args ...string) (string, error) {
	full := append([]string{"exec", "-e", "PGPASSWORD=" + password, container, "psql", "-U", user, "-d", dbName}, args...)
	out, err := exec.Command("docker", full...).CombinedOutput()
	return string(out), err
}

func psql(t *testing.T, container, user, password, dbName, sql string) {
	t.Helper()
	if out, err := dockerExecPsql(container, user, password, dbName, "-v", "ON_ERROR_STOP=1", "-c", sql); err != nil {
		t.Fatalf("psql %q failed: %v\n%s", sql, err, out)
	}
}

func psqlCount(t *testing.T, container, user, password, dbName, sql string) int {
	t.Helper()
	out, err := dockerExecPsql(container, user, password, dbName, "-tAc", sql)
	if err != nil {
		t.Fatalf("psql count %q failed: %v\n%s", sql, err, out)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("psql count %q: parse %q: %v", sql, out, err)
	}
	return n
}

func waitForPostgres(t *testing.T, container, user, password, dbName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := dockerExecPsql(container, user, password, dbName, "-tAc", "SELECT 1"); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("postgres did not accept connections within %s", timeout)
		}
		time.Sleep(time.Second)
	}
}

func waitForBackup(t *testing.T, ctx context.Context, svc backups.Service, backupID string, timeout time.Duration, agentOut *strings.Builder) backups.Backup {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		b, err := svc.GetBackup(ctx, backupID)
		if err != nil {
			t.Fatalf("GetBackup: %v", err)
		}
		if b.Status == backups.StatusSucceeded || b.Status == backups.StatusFailed {
			return b
		}
		if time.Now().After(deadline) {
			t.Fatalf("backup %s did not finish within %s (last status %q)\nagent output:\n%s", backupID, timeout, b.Status, agentOut.String())
		}
		time.Sleep(time.Second)
	}
}

func waitForRestore(t *testing.T, ctx context.Context, svc backups.Service, serviceID, restoreID string, timeout time.Duration, agentOut *strings.Builder) backups.RestoreJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		rows, err := svc.ListRestoresByService(ctx, serviceID)
		if err != nil {
			t.Fatalf("ListRestoresByService: %v", err)
		}
		for _, r := range rows {
			if r.ID != restoreID {
				continue
			}
			if r.Status == backups.RestoreStatusSucceeded || r.Status == backups.RestoreStatusFailed {
				return r
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("restore %s did not finish within %s\nagent output:\n%s", restoreID, timeout, agentOut.String())
		}
		time.Sleep(time.Second)
	}
}
