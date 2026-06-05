package projects

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

func memKey(ws, u string) string { return ws + "|" + u }

type fakeStore struct {
	insertErr    error
	got          Project
	getErr       error
	list         []Project
	members      map[string]string // memKey -> role
	usersByEmail map[string]string
}

func (f *fakeStore) InsertProject(_ context.Context, _ database.Tx, p Project) (Project, error) {
	if f.insertErr != nil {
		return Project{}, f.insertErr
	}
	p.ID = "11111111-1111-1111-1111-111111111111"
	return p, nil
}
func (f *fakeStore) GetProject(_ context.Context, _ string) (Project, error) { return f.got, f.getErr }
func (f *fakeStore) ListByWorkspace(_ context.Context, _ string) ([]Project, error) {
	return f.list, nil
}

func (f *fakeStore) InsertWorkspace(_ context.Context, _ database.Tx, name, slug string) (Workspace, error) {
	return Workspace{ID: "ws-new", Name: name, Slug: slug}, nil
}
func (f *fakeStore) AddMember(_ context.Context, _ database.Tx, ws, u, role string) error {
	if f.members == nil {
		f.members = map[string]string{}
	}
	f.members[memKey(ws, u)] = role
	return nil
}
func (f *fakeStore) MemberRole(_ context.Context, ws, u string) (string, bool, error) {
	r, ok := f.members[memKey(ws, u)]
	return r, ok, nil
}
func (f *fakeStore) ListWorkspacesForUser(_ context.Context, _ string) ([]Workspace, error) {
	return nil, nil
}
func (f *fakeStore) ListMembers(_ context.Context, _ string) ([]Member, error) { return nil, nil }
func (f *fakeStore) UpdateMemberRole(_ context.Context, _ database.Tx, ws, u, role string) error {
	if f.members == nil {
		f.members = map[string]string{}
	}
	f.members[memKey(ws, u)] = role
	return nil
}
func (f *fakeStore) RemoveMember(_ context.Context, _ database.Tx, ws, u string) error {
	delete(f.members, memKey(ws, u))
	return nil
}
func (f *fakeStore) UserIDByEmail(_ context.Context, email string) (string, bool, error) {
	u, ok := f.usersByEmail[email]
	return u, ok, nil
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

func TestCreate_WritesProjectAndAudit(t *testing.T) {
	rec := &fakeRecorder{}
	svc := newSvc(&fakeStore{}, fakeAuthz{}, rec)

	p, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: "ws1", Name: "My App"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Slug != "my-app" {
		t.Errorf("slug = %q, want my-app", p.Slug)
	}
	if !rec.called || rec.action != "project.create" {
		t.Errorf("audit not recorded correctly: called=%v action=%q", rec.called, rec.action)
	}
}

func TestCreate_DeniedWhenUnauthorized(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{err: problem.PermissionDenied("nope")}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: "ws1", Name: "App"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("got %v, want PermissionDenied", err)
	}
}

func TestCreate_RequiresName(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	if _, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: "ws1"}); err == nil {
		t.Error("expected validation error for empty name")
	}
}

func TestCreate_AuditFailurePropagates(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{recordErr: errors.New("boom")})
	if _, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: "ws1", Name: "x"}); err == nil {
		t.Error("expected error when audit recording fails (tx must not commit)")
	}
}

func TestCreate_RejectsNameWithEmptySlug(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	for _, name := range []string{"!!!", "我的应用"} {
		_, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: "ws1", Name: name})
		var pe *problem.Error
		if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
			t.Errorf("name %q: got %v, want InvalidInput", name, err)
		}
	}
}

func TestCreate_PreservesDomainErrorFromStore(t *testing.T) {
	// A unique violation surfaces from the store as problem.AlreadyExists; the service
	// must propagate it unchanged, not wrap it as Internal.
	svc := newSvc(&fakeStore{insertErr: problem.AlreadyExists("dup")}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.Create(authedCtx(), CreateInput{WorkspaceID: "ws1", Name: "My App"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindAlreadyExists {
		t.Errorf("got %v, want AlreadyExists preserved", err)
	}
}

func TestCreateWorkspace_RequiresAuth(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	_, err := svc.CreateWorkspace(context.Background(), CreateWorkspaceInput{Name: "Acme"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("anonymous CreateWorkspace: got %v, want PermissionDenied", err)
	}
}

func TestCreateWorkspace_MakesCallerOwner(t *testing.T) {
	store := &fakeStore{}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	ws, err := svc.CreateWorkspace(authedCtx(), CreateWorkspaceInput{Name: "Acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if store.members[memKey(ws.ID, "user-1")] != authz.RoleOwner {
		t.Errorf("caller should be the owner; members = %v", store.members)
	}
}

func TestInviteMember_RejectsOwnerRole(t *testing.T) {
	svc := newSvc(&fakeStore{usersByEmail: map[string]string{"x@y.com": "u9"}}, fakeAuthz{}, &fakeRecorder{})
	err := svc.InviteMember(authedCtx(), InviteMemberInput{WorkspaceID: "ws1", Email: "x@y.com", Role: "owner"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
		t.Errorf("invite as owner: got %v, want InvalidInput", err)
	}
}

func TestInviteMember_RequiresExistingAccount(t *testing.T) {
	svc := newSvc(&fakeStore{}, fakeAuthz{}, &fakeRecorder{})
	err := svc.InviteMember(authedCtx(), InviteMemberInput{WorkspaceID: "ws1", Email: "ghost@y.com", Role: "member"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("invite unknown email: got %v, want NotFound", err)
	}
}

func TestRemoveMember_CannotRemoveOwner(t *testing.T) {
	store := &fakeStore{members: map[string]string{memKey("ws1", "owner-u"): authz.RoleOwner}}
	svc := newSvc(store, fakeAuthz{}, &fakeRecorder{})
	err := svc.RemoveMember(authedCtx(), RemoveMemberInput{WorkspaceID: "ws1", UserID: "owner-u"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindPermissionDenied {
		t.Errorf("remove owner: got %v, want PermissionDenied", err)
	}
}

func TestChangeMemberRole_PromotingNonMemberFails(t *testing.T) {
	store := &fakeStore{} // no members
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)
	// Promoting a non-member to owner must fail, not silently no-op (the UPDATE would
	// match zero rows yet report success).
	err := svc.ChangeMemberRole(authedCtx(), ChangeRoleInput{WorkspaceID: "ws1", UserID: "ghost", Role: authz.RoleOwner})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindNotFound {
		t.Errorf("promote non-member to owner: got %v, want NotFound", err)
	}
	if rec.called {
		t.Error("no audit event should be written when the member doesn't exist")
	}
}

func TestChangeMemberRole_PromotesExistingMember(t *testing.T) {
	store := &fakeStore{members: map[string]string{memKey("ws1", "u2"): authz.RoleMember}}
	rec := &fakeRecorder{}
	svc := newSvc(store, fakeAuthz{}, rec)
	if err := svc.ChangeMemberRole(authedCtx(), ChangeRoleInput{WorkspaceID: "ws1", UserID: "u2", Role: authz.RoleAdmin}); err != nil {
		t.Fatalf("ChangeMemberRole: %v", err)
	}
	if got := store.members[memKey("ws1", "u2")]; got != authz.RoleAdmin {
		t.Errorf("role = %q, want admin", got)
	}
	if !rec.called || rec.action != "workspace.member.role.change" {
		t.Errorf("audit not recorded: called=%v action=%q", rec.called, rec.action)
	}
}
