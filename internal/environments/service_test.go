package environments

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const (
	testProjectID = "22222222-2222-2222-2222-222222222222"
	testEnvID     = "11111111-1111-1111-1111-111111111111"
	testWorkspace = "ws-1"
)

type fakeStore struct {
	insertErr error
	inserted  Environment
	got       Environment
	getErr    error
	list      []Environment
	wsID      string
	wsOK      bool
	wsErr     error
}

// newFakeStore resolves testProjectID to testWorkspace by default, so the
// workspace-resolution step succeeds unless a test overrides it.
func newFakeStore() *fakeStore {
	return &fakeStore{wsID: testWorkspace, wsOK: true}
}

func (f *fakeStore) InsertEnvironment(_ context.Context, _ database.Tx, e Environment) (Environment, error) {
	if f.insertErr != nil {
		return Environment{}, f.insertErr
	}
	e.ID = testEnvID
	f.inserted = e
	return e, nil
}
func (f *fakeStore) GetEnvironment(_ context.Context, _ string) (Environment, error) {
	return f.got, f.getErr
}
func (f *fakeStore) ListByProject(_ context.Context, _ string) ([]Environment, error) {
	return f.list, nil
}
func (f *fakeStore) WorkspaceIDForProject(_ context.Context, _ string) (string, bool, error) {
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

func TestCreate_WritesEnvironmentAndAudit(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(newFakeStore(), fakeAuthz{}, rec)

	e, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID, Name: "My Preview"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Slug != "my-preview" {
		t.Errorf("slug = %q, want my-preview", e.Slug)
	}
	if e.Type != "preview" {
		t.Errorf("type = %q, want preview (the default)", e.Type)
	}
	if e.WorkspaceID != testWorkspace {
		t.Errorf("workspace_id = %q, want %q", e.WorkspaceID, testWorkspace)
	}
	if !rec.called || rec.action != "environment.create" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCreate_AcceptsValidExplicitType(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	e, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID, Name: "Prod", Type: "production"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Type != "production" {
		t.Errorf("type = %q, want production", e.Type)
	}
}

func TestCreate_RejectsInvalidType(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID, Name: "X", Type: "bogus"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestCreate_DeniedWhenUnauthorized(t *testing.T) {
	store := newFakeStore()
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	_, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID, Name: "App"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a denied create must not write an audit event")
	}
	if store.inserted.ID != "" {
		t.Error("a denied create must not insert an environment")
	}
}

func TestCreate_RequiresName(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID}); err == nil {
		t.Error("expected validation error for empty name")
	}
}

func TestCreate_RequiresValidProjectID(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{ProjectID: "not-a-uuid", Name: "App"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestCreate_ProjectNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false // the parent project does not exist
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID, Name: "App"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestCreate_AuditFailurePropagates(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{recordErr: errors.New("boom")})
	if _, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID, Name: "x"}); err == nil {
		t.Error("expected error when audit recording fails (tx must not commit)")
	}
}

func TestCreate_PreservesDomainErrorFromStore(t *testing.T) {
	// A unique violation surfaces from the store as problem.AlreadyExists; the service
	// must propagate it unchanged, not wrap it as Internal.
	store := newFakeStore()
	store.insertErr = problem.AlreadyExists("dup")
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID, Name: "My App"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindAlreadyExists {
		t.Errorf("got %v, want AlreadyExists preserved", err)
	}
}

func TestCreate_RejectsNameWithEmptySlug(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{ProjectID: testProjectID, Name: "!!!"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestGet_AuthorizesAgainstWorkspace(t *testing.T) {
	store := newFakeStore()
	store.got = Environment{ID: testEnvID, ProjectID: testProjectID, WorkspaceID: testWorkspace, Name: "Preview", Slug: "preview", Type: "preview"}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	e, err := svc.Get(authedCtx(), testEnvID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.ID != testEnvID {
		t.Errorf("id = %q, want %q", e.ID, testEnvID)
	}
}

func TestGet_DeniedWhenUnauthorized(t *testing.T) {
	store := newFakeStore()
	store.got = Environment{ID: testEnvID, WorkspaceID: testWorkspace}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.Get(authedCtx(), testEnvID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
}

func TestGet_InvalidID(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Get(authedCtx(), "not-a-uuid")
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestListByProject_AuthorizesAndReturns(t *testing.T) {
	store := newFakeStore()
	store.list = []Environment{{ID: testEnvID, ProjectID: testProjectID, Name: "Preview"}}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	envs, err := svc.ListByProject(authedCtx(), testProjectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("len = %d, want 1", len(envs))
	}
}

func TestListByProject_DeniedWhenUnauthorized(t *testing.T) {
	svc := newSvc(newFakeStore(), fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.ListByProject(authedCtx(), testProjectID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
}

func TestListByProject_ProjectNotFound(t *testing.T) {
	store := newFakeStore()
	store.wsOK = false
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.ListByProject(authedCtx(), testProjectID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}
