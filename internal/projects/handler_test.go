package projects

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
)

type fakeService struct {
	list []Project
	err  error
}

func (f *fakeService) Create(_ context.Context, in CreateInput) (Project, error) {
	if f.err != nil {
		return Project{}, f.err
	}
	return Project{ID: "id1", WorkspaceID: in.WorkspaceID, Name: in.Name, Slug: "slug", CreatedAt: time.Now()}, nil
}
func (f *fakeService) Get(_ context.Context, _ string) (Project, error) {
	return Project{}, f.err
}
func (f *fakeService) ListByWorkspace(_ context.Context, _ string) ([]Project, error) {
	return f.list, f.err
}

func TestHandler_CreateProject(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.CreateProject(context.Background(),
		connect.NewRequest(&controlplanev1.CreateProjectRequest{WorkspaceId: "ws1", Name: "App"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.Msg.GetProject().GetWorkspaceId(); got != "ws1" {
		t.Errorf("workspace_id = %q, want ws1", got)
	}
}

func TestHandler_MapsDomainErrorToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.NotFound("nope")}}
	_, err := h.GetProject(context.Background(),
		connect.NewRequest(&controlplanev1.GetProjectRequest{Id: "x"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}

func TestHandler_MapsAlreadyExistsToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.AlreadyExists("dup")}}
	_, err := h.CreateProject(context.Background(),
		connect.NewRequest(&controlplanev1.CreateProjectRequest{WorkspaceId: "ws1", Name: "App"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("code = %v, want AlreadyExists", connect.CodeOf(err))
	}
}
