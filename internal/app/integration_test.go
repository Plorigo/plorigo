//go:build integration

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/plorigo/plorigo/internal/auth"
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
