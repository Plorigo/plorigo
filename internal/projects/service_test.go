package projects

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

type fakeStore struct {
	insertErr error
	got       Project
	getErr    error
	list      []Project
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

// fakeTx runs fn with a nil tx; the fakes ignore the tx value.
type fakeTx struct{}

func (fakeTx) WithinTx(_ context.Context, fn func(tx database.Tx) error) error { return fn(nil) }

func TestCreate_WritesProjectAndAudit(t *testing.T) {
	store := &fakeStore{}
	rec := &fakeRecorder{}
	svc := newService(fakeTx{}, store, rec, slog.Default())

	p, err := svc.Create(context.Background(), CreateInput{WorkspaceID: "ws1", Name: "My App"})
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

func TestCreate_RequiresName(t *testing.T) {
	svc := newService(fakeTx{}, &fakeStore{}, &fakeRecorder{}, slog.Default())
	if _, err := svc.Create(context.Background(), CreateInput{WorkspaceID: "ws1"}); err == nil {
		t.Error("expected validation error for empty name")
	}
}

func TestCreate_AuditFailurePropagates(t *testing.T) {
	svc := newService(fakeTx{}, &fakeStore{}, &fakeRecorder{recordErr: errors.New("boom")}, slog.Default())
	if _, err := svc.Create(context.Background(), CreateInput{WorkspaceID: "ws1", Name: "x"}); err == nil {
		t.Error("expected error when audit recording fails (tx must not commit)")
	}
}

func TestCreate_RejectsNameWithEmptySlug(t *testing.T) {
	svc := newService(fakeTx{}, &fakeStore{}, &fakeRecorder{}, slog.Default())
	for _, name := range []string{"!!!", "我的应用"} {
		_, err := svc.Create(context.Background(), CreateInput{WorkspaceID: "ws1", Name: name})
		var pe *problem.Error
		if !errors.As(err, &pe) || pe.Kind != problem.KindInvalidInput {
			t.Errorf("name %q: got %v, want InvalidInput", name, err)
		}
	}
}

func TestCreate_PreservesDomainErrorFromStore(t *testing.T) {
	// A unique violation surfaces from the store as problem.AlreadyExists; the service
	// must propagate it unchanged, not wrap it as Internal.
	store := &fakeStore{insertErr: problem.AlreadyExists("dup")}
	svc := newService(fakeTx{}, store, &fakeRecorder{}, slog.Default())
	_, err := svc.Create(context.Background(), CreateInput{WorkspaceID: "ws1", Name: "My App"})
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Kind != problem.KindAlreadyExists {
		t.Errorf("got %v, want AlreadyExists preserved", err)
	}
}
