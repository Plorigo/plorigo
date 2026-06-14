package envvars

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
	testServiceID = "22222222-2222-2222-2222-222222222222"
	testEnvVarID  = "11111111-1111-1111-1111-111111111111"
	testWorkspace = "ws-1"
)

type fakeStore struct {
	upsertErr error
	upserted  EnvVar
	list      []EnvVar
	deletedID string
	deletedOK bool
	deleteErr error
	wsID      string
	wsOK      bool
	wsErr     error
}

// newFakeStore resolves testServiceID to testWorkspace and deletes successfully by
// default, so the workspace-resolution and delete steps succeed unless a test overrides
// them.
func newFakeStore() *fakeStore {
	return &fakeStore{wsID: testWorkspace, wsOK: true, deletedOK: true, deletedID: testEnvVarID}
}

func (f *fakeStore) UpsertEnvVar(_ context.Context, _ database.Tx, e EnvVar) (EnvVar, error) {
	if f.upsertErr != nil {
		return EnvVar{}, f.upsertErr
	}
	e.ID = testEnvVarID
	f.upserted = e
	return e, nil
}
func (f *fakeStore) ListByService(_ context.Context, _ string) ([]EnvVar, error) {
	return f.list, nil
}
func (f *fakeStore) DeleteEnvVar(_ context.Context, _ database.Tx, _, _ string) (string, bool, error) {
	return f.deletedID, f.deletedOK, f.deleteErr
}
func (f *fakeStore) WorkspaceIDForService(_ context.Context, _ string) (string, bool, error) {
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

// fakeTx runs fn with a nil tx; the fakes ignore the tx value.
type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func authedCtx() context.Context {
	return principal.NewContext(context.Background(), principal.Principal{UserID: "user-1", Method: principal.MethodSession})
}

func newSvc(store Store, authorizer authz.Authorizer, rec Recorder) *service {
	return newService(fakeTx{}, store, authorizer, rec, slog.Default())
}

func TestSet_WritesEnvVarAndAudit(t *testing.T) {
	store := newFakeStore()
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	ev, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: "DATABASE_URL", Value: "postgres://x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Key != "DATABASE_URL" || ev.Value != "postgres://x" {
		t.Errorf("env var = %+v, want key/value preserved", ev)
	}
	if ev.WorkspaceID != testWorkspace {
		t.Errorf("workspace_id = %q, want %q", ev.WorkspaceID, testWorkspace)
	}
	if ev.ID != testEnvVarID {
		t.Errorf("id = %q, want %q", ev.ID, testEnvVarID)
	}
	if !rec.called || rec.action != "env_var.set" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestSet_TrimsKey(t *testing.T) {
	store := newFakeStore()
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	ev, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: "  PORT  ", Value: "8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Key != "PORT" {
		t.Errorf("key = %q, want PORT (trimmed)", ev.Key)
	}
	if store.upserted.Key != "PORT" {
		t.Errorf("stored key = %q, want PORT", store.upserted.Key)
	}
}

func TestSet_RejectsInvalidKey(t *testing.T) {
	cases := []string{"", "  ", "bad-key", "1ABC", "lower", "WITH SPACE", strings.Repeat("A", maxKeyLen+1)}
	for _, key := range cases {
		svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
		_, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: key, Value: "v"})
		var pe *problem.Error
		if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
			t.Errorf("key %q: got %v, want InvalidInput", key, err)
		}
	}
}

func TestSet_AllowsEmptyValue(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	ev, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: "EMPTY", Value: ""})
	if err != nil {
		t.Fatalf("empty value should be allowed, got: %v", err)
	}
	if ev.Value != "" {
		t.Errorf("value = %q, want empty", ev.Value)
	}
}

func TestSet_RejectsTooLongValue(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: "BIG", Value: strings.Repeat("a", maxValueLen+1)})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestSet_RequiresValidServiceID(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{ServiceID: "not-a-uuid", Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestSet_ServiceNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false // the parent service does not exist
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestSet_DeniedWhenUnauthorized(t *testing.T) {
	store := newFakeStore()
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	_, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a denied set must not write an audit event")
	}
	if store.upserted.ID != "" {
		t.Error("a denied set must not upsert an env var")
	}
}

func TestSet_AuditFailurePropagates(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{recordErr: errors.New("boom")})
	if _, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: "K", Value: "v"}); err == nil {
		t.Error("expected error when audit recording fails (tx must not commit)")
	}
}

func TestSet_WrapsStoreErrorAsInternal(t *testing.T) {
	store := newFakeStore()
	store.upsertErr = errors.New("db down")
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Set(authedCtx(), SetInput{ServiceID: testServiceID, Key: "K", Value: "v"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInternal {
		t.Errorf("got %v, want Internal", err)
	}
}

func TestList_AuthorizesAndReturns(t *testing.T) {
	store := newFakeStore()
	store.list = []EnvVar{{ID: testEnvVarID, ServiceID: testServiceID, Key: "A", Value: "1"}}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	vars, err := svc.List(authedCtx(), testServiceID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vars) != 1 {
		t.Fatalf("len = %d, want 1", len(vars))
	}
	if vars[0].WorkspaceID != testWorkspace {
		t.Errorf("workspace_id = %q, want %q", vars[0].WorkspaceID, testWorkspace)
	}
}

func TestList_DeniedWhenUnauthorized(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.List(authedCtx(), testServiceID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
}

func TestList_ServiceNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.List(authedCtx(), testServiceID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestList_InvalidID(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.List(authedCtx(), "not-a-uuid")
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestDelete_AuditsOnSuccess(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), fakeAuthz{}, rec)
	if err := svc.Delete(authedCtx(), DeleteInput{ServiceID: testServiceID, Key: "DATABASE_URL"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rec.called || rec.action != "env_var.delete" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestDelete_NotFoundWhenNothingDeleted(t *testing.T) {
	store := newFakeStore()
	store.deletedOK = false // no row matched
	store.deletedID = ""
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)
	err := svc.Delete(authedCtx(), DeleteInput{ServiceID: testServiceID, Key: "MISSING"})
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
	svc := newSvc(newFakeStore(), fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	err := svc.Delete(authedCtx(), DeleteInput{ServiceID: testServiceID, Key: "DATABASE_URL"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a denied delete must not write an audit event")
	}
}

func TestDelete_InvalidInput(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	if err := svc.Delete(authedCtx(), DeleteInput{ServiceID: "not-a-uuid", Key: "K"}); !isInvalid(err) {
		t.Errorf("bad service id: got %v, want InvalidInput", err)
	}
	if err := svc.Delete(authedCtx(), DeleteInput{ServiceID: testServiceID, Key: "bad-key"}); !isInvalid(err) {
		t.Errorf("bad key: got %v, want InvalidInput", err)
	}
}

func TestDelete_ServiceNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.Delete(authedCtx(), DeleteInput{ServiceID: testServiceID, Key: "K"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func isInvalid(err error) bool {
	var pe *problem.Error
	return errors.As(err, &pe) && pe.Kind == problem.KindInvalidInput
}
