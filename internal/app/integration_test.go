//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/plorigo/plorigo/internal/agents"
	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/deployments"
	"github.com/plorigo/plorigo/internal/environments"
	"github.com/plorigo/plorigo/internal/envvars"
	"github.com/plorigo/plorigo/internal/platform/config"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
	"github.com/plorigo/plorigo/internal/projects"
	"github.com/plorigo/plorigo/internal/secrets"
	"github.com/plorigo/plorigo/internal/servers"
	"github.com/plorigo/plorigo/internal/services"
)

// These tests exercise the assembled control plane against a real Postgres (CI
// provides DATABASE_URL and APP_MASTER_KEY): app assembly -> auth/policy/projects
// -> WithinTx -> sqlc. They are what catch sqlc/schema drift that compiles but
// fails at runtime, and they prove the authorization seam end-to-end.

func newApp(t *testing.T) *App {
	t.Helper()
	a, err := New(context.Background(), config.Load())
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { a.db.Close() })
	return a
}

func uniqueEmail(prefix string) string {
	return prefix + "-" + id.New().String()[:8] + "@example.com"
}

func isKind(err error, k problem.Kind) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == k
}

// registerAndLogin signs a fresh user up and logs them in — registration no longer
// auto-logs-in, so a test that needs a session logs in explicitly. Returns the login
// result (user + session) and the email.
func registerAndLogin(t *testing.T, authSvc auth.Service, ctx context.Context, prefix string) (auth.Authenticated, string) {
	t.Helper()
	email := uniqueEmail(prefix)
	if _, err := authSvc.Register(ctx, auth.RegisterInput{Email: email, Password: "supersecret"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	acct, err := authSvc.Login(ctx, auth.LoginInput{Email: email, Password: "supersecret"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return acct, email
}

func TestIntegration_RegisterBootstrapsWorkspaceAndAuthorizes(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()

	acct, _ := registerAndLogin(t, authSvc, ctx, "owner")

	// The session cookie token resolves to the user.
	p, err := authSvc.ResolveSession(ctx, acct.SessionToken)
	if err != nil || p.UserID != acct.User.ID || p.Method != principal.MethodSession {
		t.Fatalf("ResolveSession: p=%+v err=%v", p, err)
	}
	authedCtx := principal.NewContext(ctx, p)

	// Registration bootstrapped exactly one workspace, owned by the user, in one tx.
	wss, err := projSvc.ListMyWorkspaces(ctx, acct.User.ID)
	if err != nil {
		t.Fatalf("ListMyWorkspaces: %v", err)
	}
	if len(wss) != 1 {
		t.Fatalf("bootstrap workspaces = %d, want 1", len(wss))
	}
	ws := wss[0]

	var ownerRole string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, ws.ID, acct.User.ID).Scan(&ownerRole); err != nil {
		t.Fatalf("query membership: %v", err)
	}
	if ownerRole != "owner" {
		t.Fatalf("bootstrap role = %q, want owner", ownerRole)
	}

	// An authorized create succeeds and audits the REAL actor (not "system").
	proj, err := projSvc.Create(authedCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "CI App"})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	var auditActor string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT actor FROM audit_events WHERE target_id=$1 AND action='project.create'`, proj.ID).Scan(&auditActor); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditActor != acct.User.ID {
		t.Fatalf("audit actor = %q, want the registered user %q", auditActor, acct.User.ID)
	}

	// A non-member cannot create a project in someone else's workspace.
	other, _ := registerAndLogin(t, authSvc, ctx, "other")
	otherCtx := principal.NewContext(ctx, principal.Principal{UserID: other.User.ID, Method: principal.MethodSession})
	if _, err := projSvc.Create(otherCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "Sneaky"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member create: got %v, want PermissionDenied", err)
	}

	// An anonymous caller is denied as well.
	if _, err := projSvc.Create(ctx, projects.CreateInput{WorkspaceID: ws.ID, Name: "Anon"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("anonymous create: got %v, want PermissionDenied", err)
	}
}

func TestIntegration_EnvironmentScopedToProjectWorkspace(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()
	envSvc := a.environments.Service()

	owner, _ := registerAndLogin(t, authSvc, ctx, "env-owner")
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})

	wss, err := projSvc.ListMyWorkspaces(ctx, owner.User.ID)
	if err != nil || len(wss) != 1 {
		t.Fatalf("ListMyWorkspaces: wss=%d err=%v", len(wss), err)
	}
	proj, err := projSvc.Create(ownerCtx, projects.CreateInput{WorkspaceID: wss[0].ID, Name: "Env Host App"})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}

	// Create defaults the type to "preview" and audits the REAL actor against the
	// workspace resolved through the parent project (the join-based resolution).
	env, err := envSvc.Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Preview"})
	if err != nil {
		t.Fatalf("Create environment: %v", err)
	}
	if env.Type != "preview" {
		t.Fatalf("env type = %q, want preview", env.Type)
	}
	var auditActor, auditWS string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT actor, workspace_id FROM audit_events WHERE target_id=$1 AND action='environment.create'`, env.ID).Scan(&auditActor, &auditWS); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditActor != owner.User.ID {
		t.Fatalf("audit actor = %q, want %q", auditActor, owner.User.ID)
	}
	if auditWS != wss[0].ID {
		t.Fatalf("audit workspace = %q, want the parent project's workspace %q", auditWS, wss[0].ID)
	}

	// Get and ListByProject return it for the authorized owner.
	if got, err := envSvc.Get(ownerCtx, env.ID); err != nil || got.ID != env.ID {
		t.Fatalf("Get: got=%+v err=%v", got, err)
	}
	if list, err := envSvc.ListByProject(ownerCtx, proj.ID); err != nil || len(list) != 1 {
		t.Fatalf("ListByProject: len=%d err=%v", len(list), err)
	}

	// A non-member of the project's workspace is denied — proving authorization
	// resolves through the parent project, not a caller-supplied workspace id.
	other, _ := registerAndLogin(t, authSvc, ctx, "env-other")
	otherCtx := principal.NewContext(ctx, principal.Principal{UserID: other.User.ID, Method: principal.MethodSession})
	if _, err := envSvc.Create(otherCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Sneaky"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member create: got %v, want PermissionDenied", err)
	}
	if _, err := envSvc.ListByProject(otherCtx, proj.ID); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member list: got %v, want PermissionDenied", err)
	}

	// An anonymous caller is denied.
	if _, err := envSvc.Create(ctx, environments.CreateInput{ProjectID: proj.ID, Name: "Anon"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("anonymous create: got %v, want PermissionDenied", err)
	}

	// A duplicate name in the same project violates UNIQUE (project_id, slug).
	if _, err := envSvc.Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Preview"}); !isKind(err, problem.KindAlreadyExists) {
		t.Fatalf("duplicate environment: got %v, want AlreadyExists", err)
	}

	// Creating in a non-existent project resolves to no workspace -> NotFound.
	if _, err := envSvc.Create(ownerCtx, environments.CreateInput{ProjectID: id.New().String(), Name: "Ghost"}); !isKind(err, problem.KindNotFound) {
		t.Fatalf("create in missing project: got %v, want NotFound", err)
	}
}

func TestIntegration_EnvVarsScopedToEnvironmentWorkspace(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()
	envSvc := a.environments.Service()
	evSvc := a.envvars.Service()

	owner, _ := registerAndLogin(t, authSvc, ctx, "ev-owner")
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})

	wss, err := projSvc.ListMyWorkspaces(ctx, owner.User.ID)
	if err != nil || len(wss) != 1 {
		t.Fatalf("ListMyWorkspaces: wss=%d err=%v", len(wss), err)
	}
	proj, err := projSvc.Create(ownerCtx, projects.CreateInput{WorkspaceID: wss[0].ID, Name: "EV App"})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	env, err := envSvc.Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Preview"})
	if err != nil {
		t.Fatalf("Create environment: %v", err)
	}
	// Env vars are now SERVICE-scoped: create a service to hold them.
	svcRes, err := a.services.Service().CreateService(ownerCtx, services.CreateInput{EnvironmentID: env.ID, Name: "web", SourceKind: services.SourceImage, ImageRef: "nginx", ContainerPort: 80})
	if err != nil {
		t.Fatalf("Create service: %v", err)
	}
	svc := svcRes.Service

	// Set audits the REAL actor against the workspace resolved through the service (which
	// denormalizes workspace_id from environment -> project).
	ev, err := evSvc.Set(ownerCtx, envvars.SetInput{ServiceID: svc.ID, Key: "DATABASE_URL", Value: "postgres://a"})
	if err != nil {
		t.Fatalf("Set env var: %v", err)
	}
	if ev.Key != "DATABASE_URL" || ev.Value != "postgres://a" {
		t.Fatalf("env var = %+v, want key/value set", ev)
	}
	var auditActor, auditWS string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT actor, workspace_id FROM audit_events WHERE target_id=$1 AND action='env_var.set'`, ev.ID).Scan(&auditActor, &auditWS); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditActor != owner.User.ID {
		t.Fatalf("audit actor = %q, want %q", auditActor, owner.User.ID)
	}
	if auditWS != wss[0].ID {
		t.Fatalf("audit workspace = %q, want the parent project's workspace %q", auditWS, wss[0].ID)
	}

	// List returns it with the value (env vars are non-secret).
	if list, err := evSvc.List(ownerCtx, svc.ID); err != nil || len(list) != 1 || list[0].Value != "postgres://a" {
		t.Fatalf("List: list=%+v err=%v", list, err)
	}

	// Setting the same key again upserts: same row, new value, updated_at advances.
	ev2, err := evSvc.Set(ownerCtx, envvars.SetInput{ServiceID: svc.ID, Key: "DATABASE_URL", Value: "postgres://b"})
	if err != nil {
		t.Fatalf("re-Set env var: %v", err)
	}
	if ev2.ID != ev.ID {
		t.Fatalf("upsert changed the row id: got %q, want %q", ev2.ID, ev.ID)
	}
	if !ev2.UpdatedAt.After(ev.UpdatedAt) {
		t.Fatalf("updated_at did not advance: %v -> %v", ev.UpdatedAt, ev2.UpdatedAt)
	}
	if list, err := evSvc.List(ownerCtx, svc.ID); err != nil || len(list) != 1 || list[0].Value != "postgres://b" {
		t.Fatalf("after upsert List: list=%+v err=%v", list, err)
	}

	// Delete removes it; a second delete reports NotFound (not a silent no-op).
	if err := evSvc.Delete(ownerCtx, envvars.DeleteInput{ServiceID: svc.ID, Key: "DATABASE_URL"}); err != nil {
		t.Fatalf("Delete env var: %v", err)
	}
	if list, err := evSvc.List(ownerCtx, svc.ID); err != nil || len(list) != 0 {
		t.Fatalf("after delete List: list=%+v err=%v", list, err)
	}
	if err := evSvc.Delete(ownerCtx, envvars.DeleteInput{ServiceID: svc.ID, Key: "DATABASE_URL"}); !isKind(err, problem.KindNotFound) {
		t.Fatalf("double delete: got %v, want NotFound", err)
	}

	// A non-member of the service's workspace is denied for every op — proving authorization
	// resolves through the parent service, not caller-supplied input.
	other, _ := registerAndLogin(t, authSvc, ctx, "ev-other")
	otherCtx := principal.NewContext(ctx, principal.Principal{UserID: other.User.ID, Method: principal.MethodSession})
	if _, err := evSvc.Set(otherCtx, envvars.SetInput{ServiceID: svc.ID, Key: "SNEAKY", Value: "x"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member set: got %v, want PermissionDenied", err)
	}
	if _, err := evSvc.List(otherCtx, svc.ID); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member list: got %v, want PermissionDenied", err)
	}
	if err := evSvc.Delete(otherCtx, envvars.DeleteInput{ServiceID: svc.ID, Key: "DATABASE_URL"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member delete: got %v, want PermissionDenied", err)
	}

	// An anonymous caller is denied.
	if _, err := evSvc.Set(ctx, envvars.SetInput{ServiceID: svc.ID, Key: "ANON", Value: "y"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("anonymous set: got %v, want PermissionDenied", err)
	}

	// Operating on a non-existent service resolves to no workspace -> NotFound.
	if _, err := evSvc.Set(ownerCtx, envvars.SetInput{ServiceID: id.New().String(), Key: "GHOST", Value: "z"}); !isKind(err, problem.KindNotFound) {
		t.Fatalf("set on missing service: got %v, want NotFound", err)
	}
}

func TestIntegration_SecretsScopedToEnvironmentWorkspace(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()
	envSvc := a.environments.Service()
	secSvc := a.secrets.Service()

	owner, _ := registerAndLogin(t, authSvc, ctx, "sec-owner")
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})

	wss, err := projSvc.ListMyWorkspaces(ctx, owner.User.ID)
	if err != nil || len(wss) != 1 {
		t.Fatalf("ListMyWorkspaces: wss=%d err=%v", len(wss), err)
	}
	proj, err := projSvc.Create(ownerCtx, projects.CreateInput{WorkspaceID: wss[0].ID, Name: "Secret App"})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	env, err := envSvc.Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Preview"})
	if err != nil {
		t.Fatalf("Create environment: %v", err)
	}

	const plaintext = "sk_live_supersecret_value"

	// Set audits the REAL actor against the workspace resolved through the two-ancestor
	// JOIN (secret -> environment -> project -> workspace).
	sec, err := secSvc.Set(ownerCtx, secrets.SetInput{EnvironmentID: env.ID, Key: "STRIPE_SECRET_KEY", Value: plaintext})
	if err != nil {
		t.Fatalf("Set secret: %v", err)
	}
	if sec.Key != "STRIPE_SECRET_KEY" {
		t.Fatalf("secret = %+v, want key set", sec)
	}
	var auditActor, auditWS string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT actor, workspace_id FROM audit_events WHERE target_id=$1 AND action='secret.set'`, sec.ID).Scan(&auditActor, &auditWS); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditActor != owner.User.ID {
		t.Fatalf("audit actor = %q, want %q", auditActor, owner.User.ID)
	}
	if auditWS != wss[0].ID {
		t.Fatalf("audit workspace = %q, want the parent project's workspace %q", auditWS, wss[0].ID)
	}

	// Stored at rest as CIPHERTEXT — the raw column must be non-empty and must not
	// contain the plaintext. This proves the master key actually seals the value.
	var ciphertext []byte
	if err := a.db.Pool.QueryRow(ctx, `SELECT ciphertext FROM secrets WHERE id=$1`, sec.ID).Scan(&ciphertext); err != nil {
		t.Fatalf("query ciphertext: %v", err)
	}
	if len(ciphertext) == 0 {
		t.Fatal("ciphertext is empty")
	}
	if bytes.Contains(ciphertext, []byte(plaintext)) {
		t.Fatal("secret stored in the clear: the ciphertext column contains the plaintext")
	}

	// List returns metadata only — the key, never the value (Secret has no value field).
	list, err := secSvc.List(ownerCtx, env.ID)
	if err != nil || len(list) != 1 || list[0].Key != "STRIPE_SECRET_KEY" {
		t.Fatalf("List: list=%+v err=%v", list, err)
	}

	// Setting the same key again upserts: same row, re-encrypted, updated_at advances.
	sec2, err := secSvc.Set(ownerCtx, secrets.SetInput{EnvironmentID: env.ID, Key: "STRIPE_SECRET_KEY", Value: "sk_live_rotated"})
	if err != nil {
		t.Fatalf("re-Set secret: %v", err)
	}
	if sec2.ID != sec.ID {
		t.Fatalf("upsert changed the row id: got %q, want %q", sec2.ID, sec.ID)
	}
	if !sec2.UpdatedAt.After(sec.UpdatedAt) {
		t.Fatalf("updated_at did not advance: %v -> %v", sec.UpdatedAt, sec2.UpdatedAt)
	}

	// Delete removes it; a second delete reports NotFound (not a silent no-op).
	if err := secSvc.Delete(ownerCtx, secrets.DeleteInput{EnvironmentID: env.ID, Key: "STRIPE_SECRET_KEY"}); err != nil {
		t.Fatalf("Delete secret: %v", err)
	}
	if list, err := secSvc.List(ownerCtx, env.ID); err != nil || len(list) != 0 {
		t.Fatalf("after delete List: list=%+v err=%v", list, err)
	}
	if err := secSvc.Delete(ownerCtx, secrets.DeleteInput{EnvironmentID: env.ID, Key: "STRIPE_SECRET_KEY"}); !isKind(err, problem.KindNotFound) {
		t.Fatalf("double delete: got %v, want NotFound", err)
	}

	// A non-member of the environment's workspace is denied for every op — proving
	// authorization resolves through the parent environment, not caller-supplied input.
	other, _ := registerAndLogin(t, authSvc, ctx, "sec-other")
	otherCtx := principal.NewContext(ctx, principal.Principal{UserID: other.User.ID, Method: principal.MethodSession})
	if _, err := secSvc.Set(otherCtx, secrets.SetInput{EnvironmentID: env.ID, Key: "SNEAKY", Value: "x"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member set: got %v, want PermissionDenied", err)
	}
	if _, err := secSvc.List(otherCtx, env.ID); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member list: got %v, want PermissionDenied", err)
	}
	if err := secSvc.Delete(otherCtx, secrets.DeleteInput{EnvironmentID: env.ID, Key: "STRIPE_SECRET_KEY"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member delete: got %v, want PermissionDenied", err)
	}

	// An anonymous caller is denied.
	if _, err := secSvc.Set(ctx, secrets.SetInput{EnvironmentID: env.ID, Key: "ANON", Value: "y"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("anonymous set: got %v, want PermissionDenied", err)
	}

	// Operating on a non-existent environment resolves to no workspace -> NotFound.
	if _, err := secSvc.Set(ownerCtx, secrets.SetInput{EnvironmentID: id.New().String(), Key: "GHOST", Value: "z"}); !isKind(err, problem.KindNotFound) {
		t.Fatalf("set in missing environment: got %v, want NotFound", err)
	}
}

func TestIntegration_ServerScopedToWorkspace(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()
	serversSvc := a.servers.Service()

	owner, _ := registerAndLogin(t, authSvc, ctx, "srv-owner")
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})

	wss, err := projSvc.ListMyWorkspaces(ctx, owner.User.ID)
	if err != nil || len(wss) != 1 {
		t.Fatalf("ListMyWorkspaces: wss=%d err=%v", len(wss), err)
	}
	ws := wss[0]

	// An authorized create succeeds and audits the REAL actor against the workspace the
	// server is created in (servers are workspace-scoped, no parent resolution).
	srv, err := serversSvc.Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "Edge One"})
	if err != nil {
		t.Fatalf("Create server: %v", err)
	}
	if srv.Slug != "edge-one" {
		t.Fatalf("slug = %q, want edge-one", srv.Slug)
	}
	var auditActor, auditWS string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT actor, workspace_id FROM audit_events WHERE target_id=$1 AND action='server.create'`, srv.ID).Scan(&auditActor, &auditWS); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditActor != owner.User.ID {
		t.Fatalf("audit actor = %q, want %q", auditActor, owner.User.ID)
	}
	if auditWS != ws.ID {
		t.Fatalf("audit workspace = %q, want %q", auditWS, ws.ID)
	}

	// Get and ListByWorkspace return it for the authorized owner.
	if got, err := serversSvc.Get(ownerCtx, srv.ID); err != nil || got.ID != srv.ID {
		t.Fatalf("Get: got=%+v err=%v", got, err)
	}
	if list, err := serversSvc.ListByWorkspace(ownerCtx, ws.ID); err != nil || len(list) != 1 {
		t.Fatalf("ListByWorkspace: len=%d err=%v", len(list), err)
	}

	// A non-member of the workspace is denied.
	other, _ := registerAndLogin(t, authSvc, ctx, "srv-other")
	otherCtx := principal.NewContext(ctx, principal.Principal{UserID: other.User.ID, Method: principal.MethodSession})
	if _, err := serversSvc.Create(otherCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "Sneaky"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member create: got %v, want PermissionDenied", err)
	}
	if _, err := serversSvc.ListByWorkspace(otherCtx, ws.ID); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member list: got %v, want PermissionDenied", err)
	}

	// An anonymous caller is denied.
	if _, err := serversSvc.Create(ctx, servers.CreateInput{WorkspaceID: ws.ID, Name: "Anon"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("anonymous create: got %v, want PermissionDenied", err)
	}

	// A duplicate name in the same workspace violates UNIQUE (workspace_id, slug).
	if _, err := serversSvc.Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "Edge One"}); !isKind(err, problem.KindAlreadyExists) {
		t.Fatalf("duplicate server: got %v, want AlreadyExists", err)
	}

	// Delete: a non-member is denied; the owner deletes (audited with the real actor);
	// a second delete reports NotFound.
	if err := serversSvc.Delete(otherCtx, srv.ID); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member delete: got %v, want PermissionDenied", err)
	}
	if err := serversSvc.Delete(ownerCtx, srv.ID); err != nil {
		t.Fatalf("Delete server: %v", err)
	}
	var deleteActor string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT actor FROM audit_events WHERE target_id=$1 AND action='server.delete'`, srv.ID).Scan(&deleteActor); err != nil {
		t.Fatalf("query delete audit: %v", err)
	}
	if deleteActor != owner.User.ID {
		t.Fatalf("delete audit actor = %q, want %q", deleteActor, owner.User.ID)
	}
	if _, err := serversSvc.Get(ownerCtx, srv.ID); !isKind(err, problem.KindNotFound) {
		t.Fatalf("get after delete: got %v, want NotFound", err)
	}
	if err := serversSvc.Delete(ownerCtx, srv.ID); !isKind(err, problem.KindNotFound) {
		t.Fatalf("double delete: got %v, want NotFound", err)
	}
}

func TestIntegration_DeploymentScopedToEnvironmentWorkspace(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()
	envSvc := a.environments.Service()
	serversSvc := a.servers.Service()
	depSvc := a.deployments.Service()

	owner, _ := registerAndLogin(t, authSvc, ctx, "dep-owner")
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})
	wss, err := projSvc.ListMyWorkspaces(ctx, owner.User.ID)
	if err != nil || len(wss) != 1 {
		t.Fatalf("ListMyWorkspaces: wss=%d err=%v", len(wss), err)
	}
	ws := wss[0]
	proj, err := projSvc.Create(ownerCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "Dep App"})
	if err != nil {
		t.Fatalf("Create project: %v", err)
	}
	env, err := envSvc.Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Prod"})
	if err != nil {
		t.Fatalf("Create environment: %v", err)
	}
	srv, err := serversSvc.Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "Edge"})
	if err != nil {
		t.Fatalf("Create server: %v", err)
	}

	svcSvc := a.services.Service()
	mk := func(name, image string, port int32, serverID string, deploy bool) services.CreateInput {
		return services.CreateInput{EnvironmentID: env.ID, Name: name, SourceKind: services.SourceImage, ImageRef: image, ContainerPort: port, ServerID: serverID, DeployNow: deploy}
	}

	// Creating a service with deploy_now records a queued deployment, defaults the image tag,
	// denormalizes service+project+workspace, and audits the REAL actor against the workspace
	// resolved through environment -> project.
	res, err := svcSvc.CreateService(ownerCtx, mk("web", "traefik/whoami", 80, srv.ID, true))
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if res.DeploymentID == "" {
		t.Fatal("deploy_now should have enqueued a deployment")
	}
	dep, err := depSvc.Get(ownerCtx, res.DeploymentID)
	if err != nil {
		t.Fatalf("Get deployment: %v", err)
	}
	if dep.Status != deployments.StatusQueued {
		t.Fatalf("status = %q, want queued", dep.Status)
	}
	if dep.ImageRef != "traefik/whoami:latest" {
		t.Fatalf("image = %q, want :latest defaulted", dep.ImageRef)
	}
	if dep.ServiceID != res.Service.ID || dep.ProjectID != proj.ID || dep.WorkspaceID != ws.ID {
		t.Fatalf("dep = %+v, want denormalized service/project/workspace", dep)
	}
	var auditActor, auditWS string
	if err := a.db.Pool.QueryRow(ctx,
		`SELECT actor, workspace_id FROM audit_events WHERE target_id=$1 AND action='service.create'`, res.Service.ID).Scan(&auditActor, &auditWS); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditActor != owner.User.ID || auditWS != ws.ID {
		t.Fatalf("audit actor/ws = %q/%q, want %q/%q", auditActor, auditWS, owner.User.ID, ws.ID)
	}

	// Reads return the deployment for the authorized owner, by service, environment, project,
	// and workspace.
	if got, err := depSvc.Get(ownerCtx, dep.ID); err != nil || got.ID != dep.ID {
		t.Fatalf("Get: got=%+v err=%v", got, err)
	}
	if list, err := depSvc.ListByService(ownerCtx, res.Service.ID); err != nil || len(list) != 1 {
		t.Fatalf("ListByService: len=%d err=%v", len(list), err)
	}
	if list, err := depSvc.ListByEnvironment(ownerCtx, env.ID); err != nil || len(list) != 1 {
		t.Fatalf("ListByEnvironment: len=%d err=%v", len(list), err)
	}
	if list, err := depSvc.ListByProject(ownerCtx, proj.ID); err != nil || len(list) != 1 {
		t.Fatalf("ListByProject: len=%d err=%v", len(list), err)
	}
	if list, err := depSvc.ListByWorkspace(ownerCtx, ws.ID); err != nil || len(list) != 1 {
		t.Fatalf("ListByWorkspace: len=%d err=%v", len(list), err)
	}

	// Cross-tenant guard: a server in ANOTHER workspace is treated as not-found, so a caller
	// can't deploy onto infrastructure outside the service's workspace. (Fails before insert.)
	other, _ := registerAndLogin(t, authSvc, ctx, "dep-other")
	otherCtx := principal.NewContext(ctx, principal.Principal{UserID: other.User.ID, Method: principal.MethodSession})
	otherWss, _ := projSvc.ListMyWorkspaces(ctx, other.User.ID)
	otherSrv, err := serversSvc.Create(otherCtx, servers.CreateInput{WorkspaceID: otherWss[0].ID, Name: "Other Edge"})
	if err != nil {
		t.Fatalf("Create other server: %v", err)
	}
	if _, err := svcSvc.CreateService(ownerCtx, mk("web", "nginx", 80, otherSrv.ID, true)); !isKind(err, problem.KindNotFound) {
		t.Fatalf("cross-workspace server: got %v, want NotFound", err)
	}

	// A non-member of the service's workspace is denied for create and reads.
	if _, err := svcSvc.CreateService(otherCtx, mk("web", "nginx", 80, srv.ID, true)); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member create: got %v, want PermissionDenied", err)
	}
	if _, err := depSvc.ListByEnvironment(otherCtx, env.ID); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("non-member list: got %v, want PermissionDenied", err)
	}

	// An anonymous caller is denied.
	if _, err := svcSvc.CreateService(ctx, mk("web", "nginx", 80, srv.ID, true)); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("anonymous create: got %v, want PermissionDenied", err)
	}

	// Invalid input (an image service with no port) is rejected before any insert.
	if _, err := svcSvc.CreateService(ownerCtx, mk("web", "nginx", 0, srv.ID, true)); !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("bad port: got %v, want InvalidInput", err)
	}

	// Creating into a non-existent environment resolves to no workspace -> NotFound.
	if _, err := svcSvc.CreateService(ownerCtx, services.CreateInput{EnvironmentID: id.New().String(), Name: "web", SourceKind: services.SourceImage, ImageRef: "nginx", ContainerPort: 80, ServerID: srv.ID, DeployNow: true}); !isKind(err, problem.KindNotFound) {
		t.Fatalf("missing environment: got %v, want NotFound", err)
	}
}

func TestIntegration_DeploymentClaimAndReport(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()
	envSvc := a.environments.Service()
	serversSvc := a.servers.Service()
	agentsSvc := a.agents.Service()
	depSvc := a.deployments.Service()

	owner, _ := registerAndLogin(t, authSvc, ctx, "claim-owner")
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})
	wss, _ := projSvc.ListMyWorkspaces(ctx, owner.User.ID)
	ws := wss[0]
	proj, _ := projSvc.Create(ownerCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "Claim App"})
	env, _ := envSvc.Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Prod"})
	srv, _ := serversSvc.Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "Edge"})

	// Register an agent for the server to obtain a durable credential.
	tok, err := agentsSvc.CreateRegistrationToken(ownerCtx, srv.ID)
	if err != nil {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	reg, err := agentsSvc.Register(ctx, agents.RegisterInput{RegistrationToken: tok.Raw, PublicKey: pub, AgentVersion: "test"})
	if err != nil {
		t.Fatalf("Register agent: %v", err)
	}

	svcRes, err := a.services.Service().CreateService(ownerCtx, services.CreateInput{EnvironmentID: env.ID, Name: "web", SourceKind: services.SourceImage, ImageRef: "traefik/whoami", ContainerPort: 80, ServerID: srv.ID, DeployNow: true})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	dep, err := depSvc.Get(ownerCtx, svcRes.DeploymentID)
	if err != nil {
		t.Fatalf("Get deployment: %v", err)
	}

	// An unknown credential cannot poll.
	if _, err := depSvc.PollDeployment(ctx, deployments.PollInput{AgentID: reg.AgentID, Credential: "plag_bogus"}); !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("bad credential poll: got %v, want PermissionDenied", err)
	}

	// The agent claims the queued deployment exactly once (the claim is atomic). The app
	// label is the SERVICE id (the route + container key), not the environment id.
	claimed, err := depSvc.PollDeployment(ctx, deployments.PollInput{AgentID: reg.AgentID, Credential: reg.Credential})
	if err != nil {
		t.Fatalf("PollDeployment: %v", err)
	}
	if !claimed.HasWork || claimed.DeploymentID != dep.ID || claimed.AppLabel != svcRes.Service.ID {
		t.Fatalf("claimed = %+v, want the queued deployment with the service app label", claimed)
	}
	if claimed.NetworkName != "plorigo-"+env.ID || claimed.NetworkAlias != svcRes.Service.Slug {
		t.Fatalf("claimed = %+v, want per-env network + slug alias", claimed)
	}
	if again, err := depSvc.PollDeployment(ctx, deployments.PollInput{AgentID: reg.AgentID, Credential: reg.Credential}); err != nil || again.HasWork {
		t.Fatalf("second poll: again=%+v err=%v, want no work", again, err)
	}

	// Reporting running updates status + host port + container id, and records events.
	if err := depSvc.ReportDeployment(ctx, deployments.ReportInput{
		AgentID: reg.AgentID, Credential: reg.Credential, DeploymentID: dep.ID,
		Status: deployments.StatusRunning, HostPort: 32768, ContainerID: "abc123", Message: "running", LogLines: []string{"started"},
	}); err != nil {
		t.Fatalf("ReportDeployment: %v", err)
	}
	got, err := depSvc.Get(ownerCtx, dep.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != deployments.StatusRunning || got.HostPort != 32768 || got.ContainerID != "abc123" {
		t.Fatalf("after report dep = %+v, want running with host port + container id", got)
	}
	events, err := depSvc.ListEvents(ownerCtx, dep.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected timeline events to be recorded")
	}
}

func TestIntegration_GitDeploymentClaimAndReport(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()
	envSvc := a.environments.Service()
	serversSvc := a.servers.Service()
	agentsSvc := a.agents.Service()
	depSvc := a.deployments.Service()

	owner, _ := registerAndLogin(t, authSvc, ctx, "git-owner")
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})
	wss, _ := projSvc.ListMyWorkspaces(ctx, owner.User.ID)
	ws := wss[0]
	proj, _ := projSvc.Create(ownerCtx, projects.CreateInput{WorkspaceID: ws.ID, Name: "Git App"})
	env, _ := envSvc.Create(ownerCtx, environments.CreateInput{ProjectID: proj.ID, Name: "Prod"})
	srv, _ := serversSvc.Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "Edge"})

	// A public git service. Insert the row directly to keep the test off the network (the
	// services module's create path validates against GitHub; that is covered by its own tests).
	var svcID string
	if err := a.db.Pool.QueryRow(ctx,
		`INSERT INTO services (environment_id, project_id, workspace_id, name, slug, source_kind, source_access, owner, repo, full_name, branch, default_branch, html_url, container_port)
		 VALUES ($1, $2, $3, 'web', 'web', 'git', 'public', 'octocat', 'hello', 'octocat/hello', 'main', 'main', 'https://github.com/octocat/hello', 80)
		 RETURNING id`,
		env.ID, proj.ID, ws.ID).Scan(&svcID); err != nil {
		t.Fatalf("insert git service: %v", err)
	}

	// Register an agent for the server to obtain a durable credential.
	tok, err := agentsSvc.CreateRegistrationToken(ownerCtx, srv.ID)
	if err != nil {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	reg, err := agentsSvc.Register(ctx, agents.RegisterInput{RegistrationToken: tok.Raw, PublicKey: pub, AgentVersion: "test"})
	if err != nil {
		t.Fatalf("Register agent: %v", err)
	}

	// Deploying the git service records a queued git deployment with the derived clone URL and
	// the service's branch (no explicit ref override given).
	dep, err := depSvc.CreateForService(ownerCtx, deployments.CreateForServiceInput{ServiceID: svcID, ServerID: srv.ID})
	if err != nil {
		t.Fatalf("CreateForService: %v", err)
	}
	if dep.SourceKind != deployments.SourceGit || dep.SourceAccess != "public" {
		t.Fatalf("dep = %+v, want a public git deployment", dep)
	}
	if dep.CloneURL != "https://github.com/octocat/hello.git" || dep.GitRef != "main" {
		t.Fatalf("dep = %+v, want derived clone url + service-branch ref", dep)
	}

	// The agent claims it and receives the source fields + a build tag, and no image ref.
	claimed, err := depSvc.PollDeployment(ctx, deployments.PollInput{AgentID: reg.AgentID, Credential: reg.Credential})
	if err != nil {
		t.Fatalf("PollDeployment: %v", err)
	}
	if !claimed.HasWork || claimed.SourceKind != deployments.SourceGit || claimed.CloneURL != dep.CloneURL || claimed.GitRef != "main" {
		t.Fatalf("claimed = %+v, want the git source fields", claimed)
	}
	if claimed.BuiltImageTag == "" || claimed.ImageRef != "" {
		t.Fatalf("claimed = %+v, want a build tag and no pre-built image ref", claimed)
	}

	// Report the build phases, then running with the commit + built image.
	for _, st := range []string{deployments.StatusCloning, deployments.StatusBuilding} {
		if err := depSvc.ReportDeployment(ctx, deployments.ReportInput{
			AgentID: reg.AgentID, Credential: reg.Credential, DeploymentID: dep.ID, Status: st, Message: st,
		}); err != nil {
			t.Fatalf("ReportDeployment(%s): %v", st, err)
		}
	}
	if err := depSvc.ReportDeployment(ctx, deployments.ReportInput{
		AgentID: reg.AgentID, Credential: reg.Credential, DeploymentID: dep.ID,
		Status: deployments.StatusRunning, HostPort: 32790, ContainerID: "ctr-1",
		CommitSha: "abc123def456", BuiltImageRef: claimed.BuiltImageTag, Message: "running",
	}); err != nil {
		t.Fatalf("ReportDeployment(running): %v", err)
	}

	got, err := depSvc.Get(ownerCtx, dep.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != deployments.StatusRunning || got.CommitSha != "abc123def456" || got.BuiltImageRef != claimed.BuiltImageTag {
		t.Fatalf("after report dep = %+v, want running with commit + built image", got)
	}

	// Deploying a service that does not exist is NotFound.
	if _, err := depSvc.CreateForService(ownerCtx, deployments.CreateForServiceInput{ServiceID: id.New().String(), ServerID: srv.ID}); !isKind(err, problem.KindNotFound) {
		t.Fatalf("missing service: got %v, want NotFound", err)
	}
}

func TestIntegration_AgentHeartbeatRecordsHealth(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()
	projSvc := a.projects.Service()
	serversSvc := a.servers.Service()
	agentsSvc := a.agents.Service()

	owner, _ := registerAndLogin(t, authSvc, ctx, "hb-owner")
	ownerCtx := principal.NewContext(ctx, principal.Principal{UserID: owner.User.ID, Method: principal.MethodSession})
	wss, _ := projSvc.ListMyWorkspaces(ctx, owner.User.ID)
	ws := wss[0]
	srv, _ := serversSvc.Create(ownerCtx, servers.CreateInput{WorkspaceID: ws.ID, Name: "Edge"})

	tok, err := agentsSvc.CreateRegistrationToken(ownerCtx, srv.ID)
	if err != nil {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	reg, err := agentsSvc.Register(ctx, agents.RegisterInput{RegistrationToken: tok.Raw, PublicKey: pub, AgentVersion: "v1"})
	if err != nil {
		t.Fatalf("Register agent: %v", err)
	}

	// Before any heartbeat the agent has never connected: facts unknown, readiness unknown.
	pre := agentForServer(t, agentsSvc, ownerCtx, ws.ID, srv.ID)
	if pre.DockerAvailable != nil {
		t.Fatalf("pre-heartbeat DockerAvailable = %v, want nil (unknown)", *pre.DockerAvailable)
	}
	if state, _ := pre.Readiness(time.Now()); state != agents.ReadinessUnknown {
		t.Fatalf("pre-heartbeat readiness = %q, want %q", state, agents.ReadinessUnknown)
	}

	// A heartbeat carrying Docker availability records liveness AND the compatibility facts
	// in one statement — this is the sqlc round-trip the unit tests can't cover.
	dockerUp := true
	if _, err := agentsSvc.Heartbeat(ctx, agents.HeartbeatInput{
		AgentID: reg.AgentID, Credential: reg.Credential, AgentVersion: "v1",
		DockerAvailable: &dockerUp, DockerVersion: "27.1.1", OS: "linux", Arch: "amd64",
	}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	got := agentForServer(t, agentsSvc, ownerCtx, ws.ID, srv.ID)
	if got.DockerAvailable == nil || !*got.DockerAvailable {
		t.Fatalf("DockerAvailable = %v, want true", got.DockerAvailable)
	}
	if got.DockerVersion != "27.1.1" || got.OS != "linux" || got.Arch != "amd64" {
		t.Fatalf("facts = (%q, %q, %q), want (27.1.1, linux, amd64)", got.DockerVersion, got.OS, got.Arch)
	}
	if state, reason := got.Readiness(time.Now()); state != agents.ReadinessReady || reason != "" {
		t.Fatalf("readiness = (%q, %q), want (%q, empty)", state, reason, agents.ReadinessReady)
	}
}

// agentForServer returns the single agent registered to a server, via the authorized list.
func agentForServer(t *testing.T, svc agents.Service, ctx context.Context, workspaceID, serverID string) agents.Agent {
	t.Helper()
	list, err := svc.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	for _, ag := range list {
		if ag.ServerID == serverID {
			return ag
		}
	}
	t.Fatalf("no agent found for server %s", serverID)
	return agents.Agent{}
}

func TestIntegration_SessionsAndAPITokens(t *testing.T) {
	a := newApp(t)
	ctx := context.Background()
	authSvc := a.auth.Service()

	acct, email := registerAndLogin(t, authSvc, ctx, "tok")

	// API token: create -> resolve -> revoke -> no longer resolves.
	nt, err := authSvc.CreateAPIToken(ctx, acct.User.ID, "ci")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	p, err := authSvc.ResolveAPIToken(ctx, nt.Token)
	if err != nil || p.UserID != acct.User.ID || p.Method != principal.MethodAPIToken {
		t.Fatalf("ResolveAPIToken: p=%+v err=%v", p, err)
	}
	if err := authSvc.RevokeAPIToken(ctx, acct.User.ID, nt.Meta.ID); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}
	if p, _ := authSvc.ResolveAPIToken(ctx, nt.Token); p.IsAuthenticated() {
		t.Fatal("revoked API token must not resolve")
	}

	// Logout revokes the session.
	logoutCtx := principal.NewContext(ctx, principal.Principal{UserID: acct.User.ID, Method: principal.MethodSession})
	if err := authSvc.Logout(logoutCtx, acct.SessionToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if p, _ := authSvc.ResolveSession(ctx, acct.SessionToken); p.IsAuthenticated() {
		t.Fatal("session must not resolve after logout")
	}

	// Login issues a fresh working session.
	li, err := authSvc.Login(ctx, auth.LoginInput{Email: email, Password: "supersecret"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if p, _ := authSvc.ResolveSession(ctx, li.SessionToken); !p.IsAuthenticated() {
		t.Fatal("fresh login session should resolve")
	}
}
