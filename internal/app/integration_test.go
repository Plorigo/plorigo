//go:build integration

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/plorigo/plorigo/internal/auth"
	"github.com/plorigo/plorigo/internal/environments"
	"github.com/plorigo/plorigo/internal/platform/config"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
	"github.com/plorigo/plorigo/internal/projects"
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
