package secrets

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testEnvironmentID = "22222222-2222-2222-2222-222222222222"
	testSecretID      = "11111111-1111-1111-1111-111111111111"
	testWorkspace     = "ws-1"
)

type fakeStore struct {
	upsertErr      error
	upsertedKey    string
	upsertedCipher []byte
	list           []Secret
	deletedID      string
	deletedOK      bool
	deleteErr      error
	wsID           string
	wsOK           bool
	wsErr          error
}

// newFakeStore resolves testEnvironmentID to testWorkspace and deletes successfully
// by default, so the workspace-resolution and delete steps succeed unless a test
// overrides them.
func newFakeStore() *fakeStore {
	return &fakeStore{wsID: testWorkspace, wsOK: true, deletedOK: true, deletedID: testSecretID}
}

func (f *fakeStore) UpsertSecret(_ context.Context, _ database.Tx, environmentID, key string, ciphertext []byte) (Secret, error) {
	if f.upsertErr != nil {
		return Secret{}, f.upsertErr
	}
	f.upsertedKey = key
	f.upsertedCipher = ciphertext
	return Secret{ID: testSecretID, EnvironmentID: environmentID, Key: key}, nil
}
func (f *fakeStore) ListByEnvironment(_ context.Context, _ string) ([]Secret, error) {
	return f.list, nil
}
func (f *fakeStore) DeleteSecret(_ context.Context, _ database.Tx, _, _ string) (string, bool, error) {
	return f.deletedID, f.deletedOK, f.deleteErr
}
func (f *fakeStore) WorkspaceIDForEnvironment(_ context.Context, _ string) (string, bool, error) {
	return f.wsID, f.wsOK, f.wsErr
}

type fakeRecorder struct {
	called    bool
	action    string
	recordErr error
}

func (f *fakeRecorder) Record(_ context.Context, _ database.Tx, action, _, _, _, _ string) error {
	f.called = true
	f.action = action
	return f.recordErr
}

type fakeAuthz struct{ err error }

func (f fakeAuthz) Authorize(_ context.Context, _ principal.Principal, _ authz.Action, _ authz.Resource) error {
	return f.err
}

// fakeSealer records the plaintext it sealed and returns a recognizable transform, so
// tests can assert the store receives ciphertext (never the plaintext).
type fakeSealer struct {
	sealed []byte
	err    error
}

func (f *fakeSealer) Seal(plaintext []byte) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.sealed = plaintext
	return append([]byte("sealed:"), plaintext...), nil
}

// fakeTx runs fn with a nil tx; the fakes ignore the tx value.
type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: "user-1", Method: principal.MethodSession})
}

func newSvc(store Store, sealer Sealer, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, sealer, authorizer, rec, slog.Default())
}

func TestSet_SealsValueAndAudits(t *testing.T) {
	store := newFakeStore()
	sealer := &fakeSealer{}
	rec := &fakeRecorder{}
	svc := newSvc(store, sealer, fakeAuthz{}, rec)

	sec, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "STRIPE_SECRET_KEY", Value: "sk_live_123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sec.Key != "STRIPE_SECRET_KEY" || sec.ID != testSecretID {
		t.Errorf("secret = %+v, want key/id preserved", sec)
	}
	if sec.WorkspaceID != testWorkspace {
		t.Errorf("workspace_id = %q, want %q", sec.WorkspaceID, testWorkspace)
	}
	// The plaintext went through the Sealer, and the STORE received the SEALER'S OUTPUT
	// — the service never hands the store the raw value. (That a real ciphertext is not
	// the plaintext is proven against real crypto + Postgres in the integration test.)
	if string(sealer.sealed) != "sk_live_123" {
		t.Errorf("sealer received %q, want the plaintext sk_live_123", sealer.sealed)
	}
	if string(store.upsertedCipher) != "sealed:sk_live_123" {
		t.Errorf("store ciphertext = %q, want the Sealer's output", store.upsertedCipher)
	}
	if !rec.called || rec.action != "secret.set" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestSet_TrimsKey(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	sec, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "  API_TOKEN  ", Value: "v"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sec.Key != "API_TOKEN" {
		t.Errorf("key = %q, want API_TOKEN (trimmed)", sec.Key)
	}
	if store.upsertedKey != "API_TOKEN" {
		t.Errorf("stored key = %q, want API_TOKEN", store.upsertedKey)
	}
}

func TestSet_RejectsInvalidKey(t *testing.T) {
	cases := []string{"", "  ", "bad-key", "1ABC", "lower", "WITH SPACE", strings.Repeat("A", maxKeyLen+1)}
	for _, key := range cases {
		svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
		_, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: key, Value: "v"})
		var pe *problem.Error
		if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
			t.Errorf("key %q: got %v, want InvalidInput", key, err)
		}
	}
}

func TestSet_AllowsEmptyValue(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "EMPTY", Value: ""})
	if err != nil {
		t.Fatalf("empty value should be allowed, got: %v", err)
	}
}

func TestSet_RejectsTooLongValue(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "BIG", Value: strings.Repeat("a", maxValueLen+1)})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestSet_RequiresValidEnvironmentID(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{EnvironmentID: "not-a-uuid", Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestSet_EnvironmentNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false // the parent environment does not exist
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestSet_DeniedWhenUnauthorized(t *testing.T) {
	store := newFakeStore()
	sealer := &fakeSealer{}
	rec := &fakeRecorder{}
	svc := newSvc(store, sealer, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	_, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a denied set must not write an audit event")
	}
	if sealer.sealed != nil {
		t.Error("a denied set must not seal the value")
	}
	if store.upsertedCipher != nil {
		t.Error("a denied set must not upsert a secret")
	}
}

func TestSet_SealFailurePropagates(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store, &fakeSealer{err: errors.New("seal boom")}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInternal {
		t.Errorf("got %v, want Internal", err)
	}
	if store.upsertedCipher != nil {
		t.Error("a failed seal must not reach the store")
	}
}

func TestSet_AuditFailurePropagates(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{recordErr: errors.New("boom")})
	if _, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "K", Value: "v"}); err == nil {
		t.Error("expected error when audit recording fails (tx must not commit)")
	}
}

func TestSet_WrapsStoreErrorAsInternal(t *testing.T) {
	store := newFakeStore()
	store.upsertErr = errors.New("db down")
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{EnvironmentID: testEnvironmentID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInternal {
		t.Errorf("got %v, want Internal", err)
	}
}

func TestList_AuthorizesAndReturns(t *testing.T) {
	store := newFakeStore()
	store.list = []Secret{{ID: testSecretID, EnvironmentID: testEnvironmentID, Key: "A"}}
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	secs, err := svc.List(authedCtx(), testEnvironmentID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secs) != 1 {
		t.Fatalf("len = %d, want 1", len(secs))
	}
	if secs[0].WorkspaceID != testWorkspace {
		t.Errorf("workspace_id = %q, want %q", secs[0].WorkspaceID, testWorkspace)
	}
}

func TestList_DeniedWhenUnauthorized(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.List(authedCtx(), testEnvironmentID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
}

func TestList_EnvironmentNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.List(authedCtx(), testEnvironmentID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestList_InvalidID(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.List(authedCtx(), "not-a-uuid")
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestDelete_AuditsOnSuccess(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, rec)
	if err := svc.Delete(authedCtx(), DeleteInput{EnvironmentID: testEnvironmentID, Key: "STRIPE_SECRET_KEY"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rec.called || rec.action != "secret.delete" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestDelete_NotFoundWhenNothingDeleted(t *testing.T) {
	store := newFakeStore()
	store.deletedOK = false // no row matched
	store.deletedID = ""
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, rec)
	err := svc.Delete(authedCtx(), DeleteInput{EnvironmentID: testEnvironmentID, Key: "MISSING"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
	if rec.called {
		t.Error("a delete that removed nothing must not write an audit event")
	}
}

func TestDelete_DeniedWhenUnauthorized(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	err := svc.Delete(authedCtx(), DeleteInput{EnvironmentID: testEnvironmentID, Key: "STRIPE_SECRET_KEY"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a denied delete must not write an audit event")
	}
}

func TestDelete_InvalidInput(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	if err := svc.Delete(authedCtx(), DeleteInput{EnvironmentID: "not-a-uuid", Key: "K"}); !isInvalid(err) {
		t.Errorf("bad environment id: got %v, want InvalidInput", err)
	}
	if err := svc.Delete(authedCtx(), DeleteInput{EnvironmentID: testEnvironmentID, Key: "bad-key"}); !isInvalid(err) {
		t.Errorf("bad key: got %v, want InvalidInput", err)
	}
}

func TestDelete_EnvironmentNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	err := svc.Delete(authedCtx(), DeleteInput{EnvironmentID: testEnvironmentID, Key: "K"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func isInvalid(err error) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == problem.KindInvalidInput
}
