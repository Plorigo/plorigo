package githubapp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const testWS = "11111111-1111-1111-1111-111111111111"

// --- fakes ---

type fakeStore struct {
	stored   StoredConfig
	storedOK bool
	upserted *ConfigWrite
	deleted  bool
}

func (f *fakeStore) UpsertConfig(_ context.Context, _ database.Tx, w ConfigWrite) error {
	cp := w
	f.upserted = &cp
	f.stored = StoredConfig{
		AppID: w.AppID, AppSlug: w.AppSlug, ClientID: w.ClientID,
		SealedPrivateKey: w.SealedPrivateKey, SealedWebhookSecret: w.SealedWebhookSecret, SealedClientSecret: w.SealedClientSecret,
	}
	f.storedOK = true
	return nil
}
func (f *fakeStore) GetSealedConfig(_ context.Context) (StoredConfig, bool, error) {
	return f.stored, f.storedOK, nil
}
func (f *fakeStore) DeleteConfig(_ context.Context, _ database.Tx) error {
	f.deleted, f.storedOK = true, false
	return nil
}

// fakeBox seals by prefixing and opens by trimming.
type fakeBox struct{}

func (fakeBox) Seal(pt []byte) ([]byte, error) { return append([]byte("sealed:"), pt...), nil }
func (fakeBox) Open(ct []byte) ([]byte, error) { return bytes.TrimPrefix(ct, []byte("sealed:")), nil }

type fakeConverter struct {
	conv github.ManifestConversion
	err  error
}

func (f fakeConverter) ConvertManifest(_ context.Context, _ string) (github.ManifestConversion, error) {
	return f.conv, f.err
}

type fakeAuthz struct{ err error }

func (f fakeAuthz) Authorize(_ context.Context, _ principal.Principal, _ authz.Action, _ authz.Resource) error {
	return f.err
}

type fakeRecorder struct{ action string }

func (f *fakeRecorder) Record(_ context.Context, _ database.Tx, action, _, _, _, _ string) error {
	f.action = action
	return nil
}

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: "user-1", Method: principal.MethodSession})
}

func newSvc(store Store, conv ManifestConverter, authorizer authz.Authorizer, rec Recorder, env EnvConfig) *service {
	return newService(fakeTx{}, store, fakeBox{}, conv, authorizer, rec, env, "https://app.example", "https://app.example/api/github/webhook", slog.Default())
}

func isKind(err error, kind problem.Kind) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == kind
}

func nonceFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u.Query().Get("state")
}

// --- Current (credential resolution) ---

func TestCurrent_EnvTakesPrecedence(t *testing.T) {
	store := &fakeStore{stored: StoredConfig{AppID: "stored"}, storedOK: true}
	svc := newSvc(store, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{AppID: "envid", PrivateKeyPEM: "envpem", Slug: "envslug", WebhookSecret: "envsecret"})
	c, err := svc.Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if !c.Configured || c.AppID != "envid" || c.Source != "env" || c.WebhookSecret != "envsecret" {
		t.Fatalf("got %+v, want env credentials taking precedence over stored", c)
	}
}

func TestCurrent_StoredFallbackOpensSealed(t *testing.T) {
	store := &fakeStore{
		stored: StoredConfig{
			AppID: "42", AppSlug: "plorigo-x", ClientID: "Iv1",
			SealedPrivateKey: []byte("sealed:PEM"), SealedWebhookSecret: []byte("sealed:whsec"), SealedClientSecret: []byte("sealed:cs"),
		},
		storedOK: true,
	}
	svc := newSvc(store, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{})
	c, err := svc.Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if c.Source != "stored" || c.AppID != "42" || c.Slug != "plorigo-x" || c.PrivateKeyPEM != "PEM" || c.WebhookSecret != "whsec" || c.ClientSecret != "cs" {
		t.Fatalf("got %+v, want opened stored credentials", c)
	}
}

func TestCurrent_NotConfigured(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{})
	c, err := svc.Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if c.Configured {
		t.Fatalf("got %+v, want not configured", c)
	}
}

// --- BeginRegistration ---

func TestBeginRegistration_RefusesWhenEnvSet(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{AppID: "x", PrivateKeyPEM: "y"})
	_, err := svc.BeginRegistration(authedCtx(), BeginRegistrationInput{WorkspaceID: testWS})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput when the App is set via env", err)
	}
}

func TestBeginRegistration_BuildsManifestAndState(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{})
	res, err := svc.BeginRegistration(authedCtx(), BeginRegistrationInput{WorkspaceID: testWS})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !strings.Contains(res.FormAction, "github.com/settings/apps/new?state=") {
		t.Errorf("form action = %q, want the user create-app page with state", res.FormAction)
	}
	if !strings.Contains(res.ManifestJSON, "/api/github/app/manifest/callback") || !strings.Contains(res.ManifestJSON, "pull_request") {
		t.Errorf("manifest = %q, want redirect_url + pull_request events", res.ManifestJSON)
	}
	if res.State == "" || nonceFromURL(t, res.FormAction) == "" {
		t.Error("want a sealed state cookie + a nonce echoed in the form action")
	}
}

func TestBeginRegistration_OrgFormAction(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{})
	res, err := svc.BeginRegistration(authedCtx(), BeginRegistrationInput{WorkspaceID: testWS, Org: "acme"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !strings.Contains(res.FormAction, "github.com/organizations/acme/settings/apps/new") {
		t.Errorf("form action = %q, want the org create-app page", res.FormAction)
	}
}

func TestBeginRegistration_Denied(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeConverter{}, fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{}, EnvConfig{})
	_, err := svc.BeginRegistration(authedCtx(), BeginRegistrationInput{WorkspaceID: testWS})
	if !isKind(err, problem.KindPermissionDenied) {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
}

// --- CompleteRegistration ---

func TestCompleteRegistration_ConvertsSealsStoresAudits(t *testing.T) {
	store := &fakeStore{}
	rec := &fakeRecorder{}
	conv := fakeConverter{conv: github.ManifestConversion{AppID: 42, Slug: "plorigo-x", PrivateKeyPEM: "PEMDATA", WebhookSecret: "whsec", ClientID: "Iv1", ClientSecret: "cs"}}
	svc := newSvc(store, conv, fakeAuthz{}, rec, EnvConfig{})

	begin, err := svc.BeginRegistration(authedCtx(), BeginRegistrationInput{WorkspaceID: testWS})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	res, err := svc.CompleteRegistration(authedCtx(), CompleteRegistrationInput{Code: "tempcode", State: nonceFromURL(t, begin.FormAction), CookieState: begin.State})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Slug != "plorigo-x" || res.AppID != "42" {
		t.Errorf("result = %+v, want the new app's slug + id", res)
	}
	// Credentials are sealed before storage (never the plaintext).
	if store.upserted == nil || store.upserted.AppID != "42" || string(store.upserted.SealedPrivateKey) != "sealed:PEMDATA" || string(store.upserted.SealedWebhookSecret) != "sealed:whsec" {
		t.Errorf("upserted = %+v, want sealed key + secret", store.upserted)
	}
	if rec.action != "github_app.register" {
		t.Errorf("audit action = %q, want github_app.register", rec.action)
	}
	// The cache is cleared, so the next resolve reflects the freshly-stored app (opened).
	c, _ := svc.Current(context.Background())
	if !c.Configured || c.Source != "stored" || c.AppID != "42" || c.PrivateKeyPEM != "PEMDATA" {
		t.Errorf("post-register Current = %+v, want the stored app", c)
	}
}

func TestCompleteRegistration_StateMismatchRejected(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{})
	begin, err := svc.BeginRegistration(authedCtx(), BeginRegistrationInput{WorkspaceID: testWS})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = svc.CompleteRegistration(authedCtx(), CompleteRegistrationInput{Code: "c", State: "wrong-nonce", CookieState: begin.State})
	if !isKind(err, problem.KindInvalidInput) {
		t.Fatalf("got %v, want InvalidInput on a state mismatch", err)
	}
}

// --- resolvers ---

func TestResolvers_FromStored(t *testing.T) {
	store := &fakeStore{
		stored:   StoredConfig{AppID: "42", AppSlug: "plorigo-x", SealedPrivateKey: []byte("sealed:PEM"), SealedWebhookSecret: []byte("sealed:whsec")},
		storedOK: true,
	}
	svc := newSvc(store, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{})

	id, pem, ok := svc.AppCredentials(context.Background())
	if !ok || id != "42" || pem != "PEM" {
		t.Errorf("AppCredentials = %q,%q,%v", id, pem, ok)
	}
	if got := svc.WebhookSecret(context.Background()); got != "whsec" {
		t.Errorf("WebhookSecret = %q", got)
	}
	if _, slug, configured := svc.AppConfig(context.Background()); !configured || slug != "plorigo-x" {
		t.Errorf("AppConfig slug=%q configured=%v", slug, configured)
	}
	u, ok := svc.InstallURL(context.Background(), "nonce123")
	if !ok || !strings.Contains(u, "github.com/apps/plorigo-x/installations/new?state=nonce123") {
		t.Errorf("InstallURL = %q ok=%v", u, ok)
	}
}

func TestResolvers_NotConfigured(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeConverter{}, fakeAuthz{}, &fakeRecorder{}, EnvConfig{})
	if _, _, ok := svc.AppCredentials(context.Background()); ok {
		t.Error("AppCredentials ok should be false when not configured")
	}
	if got := svc.WebhookSecret(context.Background()); got != "" {
		t.Errorf("WebhookSecret = %q, want empty (fails closed)", got)
	}
	if _, ok := svc.InstallURL(context.Background(), "n"); ok {
		t.Error("InstallURL ok should be false when not configured")
	}
}
