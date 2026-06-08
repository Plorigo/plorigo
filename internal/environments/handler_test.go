package environments

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
)

type fakeService struct {
	list []Environment
	err  error
}

func (f *fakeService) Create(_ context.Context, in CreateInput) (Environment, error) {
	if f.err != nil {
		return Environment{}, f.err
	}
	return Environment{ID: "id1", ProjectID: in.ProjectID, Name: in.Name, Slug: "slug", Type: "preview", CreatedAt: time.Now()}, nil
}
func (f *fakeService) Get(_ context.Context, _ string) (Environment, error) {
	return Environment{}, f.err
}
func (f *fakeService) ListByProject(_ context.Context, _ string) ([]Environment, error) {
	return f.list, f.err
}

func TestHandler_CreateEnvironment(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.CreateEnvironment(context.Background(),
		connect.NewRequest(&controlplanev1.CreateEnvironmentRequest{ProjectId: "p1", Name: "Preview"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.Msg.GetEnvironment().GetProjectId(); got != "p1" {
		t.Errorf("project_id = %q, want p1", got)
	}
}

func TestHandler_MapsDomainErrorToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.NotFound("nope")}}
	_, err := h.GetEnvironment(context.Background(),
		connect.NewRequest(&controlplanev1.GetEnvironmentRequest{Id: "x"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}

func TestHandler_MapsAlreadyExistsToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.AlreadyExists("dup")}}
	_, err := h.CreateEnvironment(context.Background(),
		connect.NewRequest(&controlplanev1.CreateEnvironmentRequest{ProjectId: "p1", Name: "Preview"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("code = %v, want AlreadyExists", connect.CodeOf(err))
	}
}
