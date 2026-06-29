package sources

import (
	"net/url"
	"strings"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

func stateFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse install url: %v", err)
	}
	return u.Query().Get("state")
}

func TestBeginAppInstall_ReturnsInstallURLAndState(t *testing.T) {
	svc := newSvcApp(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testApp(), fakeAuthz{}, &fakeRecorder{})
	res, err := svc.BeginAppInstall(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !strings.Contains(res.AuthorizeURL, "github.com/apps/plorigo-test/installations/new") {
		t.Errorf("install url = %q, want the app's install page", res.AuthorizeURL)
	}
	if stateFromURL(t, res.AuthorizeURL) == "" || res.State == "" {
		t.Error("begin should carry a state nonce + sealed cookie state")
	}
}

func TestBeginAppInstall_NotConfigured(t *testing.T) {
	svc := newSvcApp(newFakeStore(), &fakeBox{}, &fakeGitHub{}, AppConfig{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.BeginAppInstall(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("err = %v, want InvalidInput when the App is not configured", err)
	}
}

func TestBeginAppInstall_Unauthorized(t *testing.T) {
	svc := newSvcApp(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testApp(), fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.BeginAppInstall(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("err = %v, want PermissionDenied", err)
	}
}

func TestCompleteAppInstall_StoresInstallation(t *testing.T) {
	store := newFakeStore()
	gh := &fakeGitHub{installation: github.Installation{ID: 99, Account: "acme-org", AccountID: 7}}
	rec := &fakeRecorder{}
	svc := newSvcApp(store, &fakeBox{}, gh, testApp(), fakeAuthz{}, rec)

	begin, err := svc.BeginAppInstall(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	nonce := stateFromURL(t, begin.AuthorizeURL)
	res, err := svc.CompleteAppInstall(authedCtx(), CompleteAppInput{
		InstallationID: "42", SetupAction: "install", State: nonce, CookieState: begin.State,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.GitHubLogin != "acme-org" || res.WorkspaceID != testWorkspaceID {
		t.Fatalf("unexpected result: %+v", res)
	}
	if store.upsertedApp == nil || store.upsertedApp.InstallationID != "42" || store.upsertedApp.GitHubLogin != "acme-org" {
		t.Fatalf("app connection not upserted correctly: %+v", store.upsertedApp)
	}
	if !rec.called || rec.action != "source.github_app.connect" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCompleteAppInstall_StateMismatch(t *testing.T) {
	svc := newSvcApp(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testApp(), fakeAuthz{}, &fakeRecorder{})
	begin, _ := svc.BeginAppInstall(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	_, err := svc.CompleteAppInstall(authedCtx(), CompleteAppInput{InstallationID: "42", State: "wrong", CookieState: begin.State})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("err = %v, want InvalidInput on a state mismatch", err)
	}
}

func TestCompleteAppInstall_DifferentUserRejected(t *testing.T) {
	svc := newSvcApp(newFakeStore(), &fakeBox{}, &fakeGitHub{}, testApp(), fakeAuthz{}, &fakeRecorder{})
	begin, _ := svc.BeginAppInstall(authedCtx(), BeginAuthInput{WorkspaceID: testWorkspaceID})
	nonce := stateFromURL(t, begin.AuthorizeURL)
	// A different session completes the install started by testUserID.
	_, err := svc.CompleteAppInstall(authedCtxFor("someone-else"), CompleteAppInput{InstallationID: "42", State: nonce, CookieState: begin.State})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("err = %v, want PermissionDenied for a different user", err)
	}
}

func TestInstallationToken_ResolvesAndMints(t *testing.T) {
	store := newFakeStore()
	store.appInstallationID = "42"
	store.appInstallOK = true
	gh := &fakeGitHub{installToken: "ghs_inst"}
	svc := newSvcApp(store, &fakeBox{}, gh, testApp(), fakeAuthz{}, &fakeRecorder{})

	tok, ok, err := svc.InstallationToken(authedCtx(), testWorkspaceID)
	if err != nil {
		t.Fatalf("InstallationToken: %v", err)
	}
	if !ok || tok != "ghs_inst" {
		t.Fatalf("token=%q ok=%v, want the minted installation token", tok, ok)
	}
}

func TestInstallationToken_NoInstallation(t *testing.T) {
	store := newFakeStore()
	store.appInstallOK = false
	svc := newSvcApp(store, &fakeBox{}, &fakeGitHub{}, testApp(), fakeAuthz{}, &fakeRecorder{})

	_, ok, err := svc.InstallationToken(authedCtx(), testWorkspaceID)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Error("ok should be false when no App installation is connected")
	}
}
