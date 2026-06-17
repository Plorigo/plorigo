package config

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
	testServiceID     = "33333333-3333-3333-3333-333333333333"
	testEnvironmentID = "22222222-2222-2222-2222-222222222222"
	testEntryID       = "11111111-1111-1111-1111-111111111111"
	testWorkspace     = "ws-1"
)

type fakeStore struct {
	upsertErr      error
	svcUpserted    bool
	envUpserted    bool
	upsertedKey    string
	upsertedValue  *string
	upsertedCipher []byte
	list           []Entry
	deletedID      string
	deletedOK      bool
	deleteErr      error
	wsOK           bool
	wsErr          error
}

// newFakeStore resolves the test service/environment to testWorkspace and deletes
// successfully by default, so the workspace-resolution and delete steps succeed unless a
// test overrides them.
func newFakeStore() *fakeStore {
	return &fakeStore{wsOK: true, deletedOK: true, deletedID: testEntryID}
}

func (f *fakeStore) UpsertServiceConfig(_ context.Context, _ database.Tx, typ Type, serviceID, key string, value *string, ciphertext []byte) (Entry, error) {
	if f.upsertErr != nil {
		return Entry{}, f.upsertErr
	}
	f.svcUpserted = true
	f.upsertedKey = key
	f.upsertedValue = value
	f.upsertedCipher = ciphertext
	return Entry{ID: testEntryID, Type: typ, Scope: ScopeService, ServiceID: serviceID, Key: key, Value: deref(value)}, nil
}

func (f *fakeStore) UpsertEnvironmentConfig(_ context.Context, _ database.Tx, typ Type, environmentID, key string, value *string, ciphertext []byte) (Entry, error) {
	if f.upsertErr != nil {
		return Entry{}, f.upsertErr
	}
	f.envUpserted = true
	f.upsertedKey = key
	f.upsertedValue = value
	f.upsertedCipher = ciphertext
	return Entry{ID: testEntryID, Type: typ, Scope: ScopeEnvironment, EnvironmentID: environmentID, Key: key, Value: deref(value)}, nil
}

func (f *fakeStore) ListForService(_ context.Context, _ string) ([]Entry, error) { return f.list, nil }

func (f *fakeStore) DeleteServiceConfig(_ context.Context, _ database.Tx, _, _ string) (string, bool, error) {
	return f.deletedID, f.deletedOK, f.deleteErr
}

func (f *fakeStore) DeleteEnvironmentConfig(_ context.Context, _ database.Tx, _, _ string) (string, bool, error) {
	return f.deletedID, f.deletedOK, f.deleteErr
}

func (f *fakeStore) WorkspaceIDForService(_ context.Context, _ string) (string, bool, error) {
	return testWorkspace, f.wsOK, f.wsErr
}

func (f *fakeStore) WorkspaceIDForEnvironment(_ context.Context, _ string) (string, bool, error) {
	return testWorkspace, f.wsOK, f.wsErr
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
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

// fakeSealer records the plaintext it sealed and returns a recognizable transform, so tests
// can assert the store receives ciphertext (never the plaintext).
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

type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: "user-1", Method: principal.MethodSession})
}

func newSvc(store Store, sealer Sealer, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, sealer, authorizer, rec, slog.Default())
}

func TestSet_VariableServiceStoresPlaintext(t *testing.T) {
	store := newFakeStore()
	sealer := &fakeSealer{}
	rec := &fakeRecorder{}
	svc := newSvc(store, sealer, fakeAuthz{}, rec)

	e, err := svc.Set(authedCtx(), SetInput{Type: TypeVariable, Scope: ScopeService, ServiceID: testServiceID, Key: "PORT", Value: "8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !store.svcUpserted || store.envUpserted {
		t.Errorf("wrong scope target: svc=%v env=%v", store.svcUpserted, store.envUpserted)
	}
	if deref(store.upsertedValue) != "8080" || store.upsertedCipher != nil {
		t.Errorf("variable should store plaintext value and no ciphertext: value=%v cipher=%v", store.upsertedValue, store.upsertedCipher)
	}
	if sealer.sealed != nil {
		t.Error("a variable must not be sealed")
	}
	if e.Value != "8080" {
		t.Errorf("returned value = %q, want plaintext for a variable", e.Value)
	}
	if e.WorkspaceID != testWorkspace || !rec.called || rec.action != "config.set" {
		t.Errorf("workspace/audit wrong: ws=%q called=%v action=%q", e.WorkspaceID, rec.called, rec.action)
	}
}

func TestSet_SecretEnvironmentSealsValue(t *testing.T) {
	store := newFakeStore()
	sealer := &fakeSealer{}
	rec := &fakeRecorder{}
	svc := newSvc(store, sealer, fakeAuthz{}, rec)

	e, err := svc.Set(authedCtx(), SetInput{Type: TypeSecret, Scope: ScopeEnvironment, EnvironmentID: testEnvironmentID, Key: "DATABASE_URL", Value: "postgres://x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !store.envUpserted || store.svcUpserted {
		t.Errorf("wrong scope target: svc=%v env=%v", store.svcUpserted, store.envUpserted)
	}
	if string(sealer.sealed) != "postgres://x" {
		t.Errorf("sealer received %q, want the plaintext", sealer.sealed)
	}
	if string(store.upsertedCipher) != "sealed:postgres://x" || store.upsertedValue != nil {
		t.Errorf("secret should store ciphertext and no plaintext: cipher=%q value=%v", store.upsertedCipher, store.upsertedValue)
	}
	if e.Value != "" {
		t.Errorf("returned secret value = %q, want empty (write-only)", e.Value)
	}
	if rec.action != "config.set" {
		t.Errorf("audit action = %q, want config.set", rec.action)
	}
}

func TestSet_RejectsBadTypeOrScope(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.Set(authedCtx(), SetInput{Type: "nope", Scope: ScopeService, ServiceID: testServiceID, Key: "K", Value: "v"}); !isInvalid(err) {
		t.Errorf("bad type: got %v, want InvalidInput", err)
	}
	if _, err := svc.Set(authedCtx(), SetInput{Type: TypeVariable, Scope: "nope", ServiceID: testServiceID, Key: "K", Value: "v"}); !isInvalid(err) {
		t.Errorf("bad scope: got %v, want InvalidInput", err)
	}
}

func TestSet_RejectsInvalidKey(t *testing.T) {
	cases := []string{"", "  ", "bad-key", "1ABC", "lower", "WITH SPACE", strings.Repeat("A", maxKeyLen+1)}
	for _, key := range cases {
		svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
		_, err := svc.Set(authedCtx(), SetInput{Type: TypeVariable, Scope: ScopeService, ServiceID: testServiceID, Key: key, Value: "v"})
		if !isInvalid(err) {
			t.Errorf("key %q: got %v, want InvalidInput", key, err)
		}
	}
}

func TestSet_RejectsTooLongValue(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{Type: TypeVariable, Scope: ScopeService, ServiceID: testServiceID, Key: "BIG", Value: strings.Repeat("a", maxValueLen+1)})
	if !isInvalid(err) {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestSet_RequiresScopeTargetID(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.Set(authedCtx(), SetInput{Type: TypeVariable, Scope: ScopeService, ServiceID: "not-a-uuid", Key: "K", Value: "v"}); !isInvalid(err) {
		t.Errorf("service scope without valid service_id: got %v, want InvalidInput", err)
	}
	if _, err := svc.Set(authedCtx(), SetInput{Type: TypeSecret, Scope: ScopeEnvironment, EnvironmentID: "not-a-uuid", Key: "K", Value: "v"}); !isInvalid(err) {
		t.Errorf("environment scope without valid environment_id: got %v, want InvalidInput", err)
	}
}

func TestSet_NotFoundWhenScopeTargetMissing(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{Type: TypeVariable, Scope: ScopeService, ServiceID: testServiceID, Key: "K", Value: "v"})
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
	_, err := svc.Set(authedCtx(), SetInput{Type: TypeSecret, Scope: ScopeEnvironment, EnvironmentID: testEnvironmentID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if rec.called || sealer.sealed != nil || store.envUpserted {
		t.Error("a denied set must not seal, upsert, or audit")
	}
}

func TestSet_SealFailurePropagates(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store, &fakeSealer{err: errors.New("seal boom")}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{Type: TypeSecret, Scope: ScopeService, ServiceID: testServiceID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInternal {
		t.Errorf("got %v, want Internal", err)
	}
	if store.svcUpserted {
		t.Error("a failed seal must not reach the store")
	}
}

func TestSet_AuditFailurePropagates(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{recordErr: errors.New("boom")})
	if _, err := svc.Set(authedCtx(), SetInput{Type: TypeVariable, Scope: ScopeService, ServiceID: testServiceID, Key: "K", Value: "v"}); err == nil {
		t.Error("expected error when audit recording fails (tx must not commit)")
	}
}

func TestListForService_AuthorizesAndReturns(t *testing.T) {
	store := newFakeStore()
	store.list = []Entry{
		{ID: testEntryID, Type: TypeVariable, Scope: ScopeService, ServiceID: testServiceID, Key: "PORT", Value: "8080"},
		{ID: "e2", Type: TypeSecret, Scope: ScopeEnvironment, EnvironmentID: testEnvironmentID, Key: "DATABASE_URL"},
	}
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	entries, err := svc.ListForService(authedCtx(), testServiceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	for _, e := range entries {
		if e.WorkspaceID != testWorkspace {
			t.Errorf("workspace_id = %q, want %q", e.WorkspaceID, testWorkspace)
		}
	}
}

func TestListForService_InvalidNotFoundDenied(t *testing.T) {
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.ListForService(authedCtx(), "not-a-uuid"); !isInvalid(err) {
		t.Errorf("invalid id: got %v, want InvalidInput", err)
	}

	missing := newFakeStore()
	missing.wsOK = false
	svc = newSvc(missing, &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	var pe *problem.Error
	if _, err := svc.ListForService(authedCtx(), testServiceID); !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("missing service: got %v, want NotFound", err)
	}

	svc = newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	if _, err := svc.ListForService(authedCtx(), testServiceID); !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("unauthorized: got %v, want PermissionDenied", err)
	}
}

func TestDelete_AuditsOnSuccess(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, rec)
	if err := svc.Delete(authedCtx(), DeleteInput{Scope: ScopeService, ServiceID: testServiceID, Key: "PORT"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rec.called || rec.action != "config.delete" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestDelete_NotFoundWhenNothingDeleted(t *testing.T) {
	store := newFakeStore()
	store.deletedOK = false
	store.deletedID = ""
	rec := &fakeRecorder{}
	svc := newSvc(store, &fakeSealer{}, fakeAuthz{}, rec)
	err := svc.Delete(authedCtx(), DeleteInput{Scope: ScopeEnvironment, EnvironmentID: testEnvironmentID, Key: "MISSING"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
	if rec.called {
		t.Error("a delete that removed nothing must not write an audit event")
	}
}

func TestDelete_DeniedAndInvalid(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	if err := svc.Delete(authedCtx(), DeleteInput{Scope: ScopeService, ServiceID: testServiceID, Key: "PORT"}); !errors.As(err, new(*problem.Error)) {
		t.Fatalf("expected problem error, got %v", err)
	}
	if rec.called {
		t.Error("a denied delete must not write an audit event")
	}

	svc = newSvc(newFakeStore(), &fakeSealer{}, fakeAuthz{}, &fakeRecorder{})
	if err := svc.Delete(authedCtx(), DeleteInput{Scope: ScopeService, ServiceID: "not-a-uuid", Key: "K"}); !isInvalid(err) {
		t.Errorf("bad service id: got %v, want InvalidInput", err)
	}
	if err := svc.Delete(authedCtx(), DeleteInput{Scope: ScopeService, ServiceID: testServiceID, Key: "bad-key"}); !isInvalid(err) {
		t.Errorf("bad key: got %v, want InvalidInput", err)
	}
}

func isInvalid(err error) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == problem.KindInvalidInput
}
