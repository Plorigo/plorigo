package servers

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
	testServerID  = "11111111-1111-1111-1111-111111111111"
	testWorkspace = "ws-1"
)

type fakeStore struct {
	insertErr error
	inserted  Server
	got       Server
	getErr    error
	list      []Server
	deleteOK  bool
	deleted   bool
}

func (f *fakeStore) InsertServer(_ context.Context, _ database.Tx, s Server) (Server, error) {
	if f.insertErr != nil {
		return Server{}, f.insertErr
	}
	s.ID = testServerID
	f.inserted = s
	return s, nil
}
func (f *fakeStore) GetServer(_ context.Context, _ string) (Server, error) {
	return f.got, f.getErr
}
func (f *fakeStore) ListByWorkspace(_ context.Context, _ string) ([]Server, error) {
	return f.list, nil
}
func (f *fakeStore) DeleteServer(_ context.Context, _ database.Tx, _ string) (bool, error) {
	f.deleted = f.deleteOK
	return f.deleteOK, nil
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

func TestCreate_WritesServerAndAudit(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(&fakeStore{}, fakeAuthz{}, rec)

	srv, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: testWorkspace, Name: "Edge One"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.Slug != "edge-one" {
		t.Errorf("slug = %q, want edge-one", srv.Slug)
	}
	if srv.WorkspaceID != testWorkspace {
		t.Errorf("workspace_id = %q, want %q", srv.WorkspaceID, testWorkspace)
	}
	if !rec.called || rec.action != "server.create" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCreate_DeniedWhenUnauthorized(t *testing.T) {
	store := &fakeStore{}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)
	_, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: testWorkspace, Name: "Box"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if rec.called {
		t.Error("a denied create must not write an audit event")
	}
	if store.inserted.ID != "" {
		t.Error("a denied create must not insert a server")
	}
}

func TestCreate_RequiresName(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: testWorkspace}); err == nil {
		t.Error("expected validation error for empty name")
	}
}

func TestCreate_RequiresWorkspaceID(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{Name: "Box"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestCreate_RejectsNameWithEmptySlug(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: testWorkspace, Name: "!!!"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestCreate_AuditFailurePropagates(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{recordErr: errors.New("boom")})
	if _, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: testWorkspace, Name: "x"}); err == nil {
		t.Error("expected error when audit recording fails (tx must not commit)")
	}
}

func TestCreate_PreservesDomainErrorFromStore(t *testing.T) {
	// A unique violation surfaces from the store as problem.AlreadyExists; the service
	// must propagate it unchanged, not wrap it as Internal.
	store := &fakeStore{insertErr: problem.AlreadyExists("dup")}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: testWorkspace, Name: "Box"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindAlreadyExists {
		t.Errorf("got %v, want AlreadyExists preserved", err)
	}
}

func TestGet_AuthorizesAgainstWorkspace(t *testing.T) {
	store := &fakeStore{got: Server{ID: testServerID, WorkspaceID: testWorkspace, Name: "Edge", Slug: "edge"}}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	srv, err := svc.Get(authedCtx(), testServerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.ID != testServerID {
		t.Errorf("id = %q, want %q", srv.ID, testServerID)
	}
}

func TestGet_DeniedWhenUnauthorized(t *testing.T) {
	store := &fakeStore{got: Server{ID: testServerID, WorkspaceID: testWorkspace}}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.Get(authedCtx(), testServerID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
}

func TestGet_InvalidID(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Get(authedCtx(), "not-a-uuid")
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestListByWorkspace_AuthorizesAndReturns(t *testing.T) {
	store := &fakeStore{list: []Server{{ID: testServerID, WorkspaceID: testWorkspace, Name: "Edge"}}}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	srvs, err := svc.ListByWorkspace(authedCtx(), testWorkspace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(srvs) != 1 {
		t.Fatalf("len = %d, want 1", len(srvs))
	}
}

func TestListByWorkspace_DeniedWhenUnauthorized(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.ListByWorkspace(authedCtx(), testWorkspace)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
}

func TestListByWorkspace_RequiresWorkspaceID(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.ListByWorkspace(authedCtx(), "")
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}

func TestDelete_AuthorizedDeletesAndAudits(t *testing.T) {
	store := &fakeStore{got: Server{ID: testServerID, WorkspaceID: testWorkspace}, deleteOK: true}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	if err := svc.Delete(authedCtx(), testServerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !store.deleted {
		t.Error("expected the server to be deleted")
	}
	if !rec.called || rec.action != "server.delete" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestDelete_DeniedWhenUnauthorized(t *testing.T) {
	store := &fakeStore{got: Server{ID: testServerID, WorkspaceID: testWorkspace}, deleteOK: true}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{err: problem.PermissionDenied("nope")}, rec)

	err := svc.Delete(authedCtx(), testServerID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
	if store.deleted {
		t.Error("a denied delete must not remove the server")
	}
	if rec.called {
		t.Error("a denied delete must not write an audit event")
	}
}

func TestDelete_MissingServerNotFound(t *testing.T) {
	store := &fakeStore{getErr: problem.NotFound("server %s not found", testServerID)}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.Delete(authedCtx(), testServerID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
}

func TestDelete_RacedDeleteNotFoundAndNotAudited(t *testing.T) {
	// The row vanished between Get and the tx (raced with another delete): the store
	// reports nothing deleted, so the service returns NotFound and audits nothing.
	store := &fakeStore{got: Server{ID: testServerID, WorkspaceID: testWorkspace}, deleteOK: false}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)

	err := svc.Delete(authedCtx(), testServerID)
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("got %v, want NotFound", err)
	}
	if rec.called {
		t.Error("a delete that removed nothing must not be audited")
	}
}

func TestDelete_InvalidID(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	err := svc.Delete(authedCtx(), "not-a-uuid")
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("got %v, want InvalidInput", err)
	}
}
