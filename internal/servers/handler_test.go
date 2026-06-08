package servers

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
)

type fakeService struct {
	list []Server
	err  error
}

func (f *fakeService) Create(_ context.Context, in CreateInput) (Server, error) {
	if f.err != nil {
		return Server{}, f.err
	}
	return Server{ID: "id1", WorkspaceID: in.WorkspaceID, Name: in.Name, Slug: "slug", CreatedAt: time.Now()}, nil
}
func (f *fakeService) Get(_ context.Context, _ string) (Server, error) {
	return Server{}, f.err
}
func (f *fakeService) ListByWorkspace(_ context.Context, _ string) ([]Server, error) {
	return f.list, f.err
}

func TestHandler_CreateServer(t *testing.T) {
	h := &handler{svc: &fakeService{}}
	resp, err := h.CreateServer(context.Background(),
		connect.NewRequest(&controlplanev1.CreateServerRequest{WorkspaceId: "w1", Name: "Edge"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.Msg.GetServer().GetWorkspaceId(); got != "w1" {
		t.Errorf("workspace_id = %q, want w1", got)
	}
}

func TestHandler_MapsDomainErrorToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.NotFound("nope")}}
	_, err := h.GetServer(context.Background(),
		connect.NewRequest(&controlplanev1.GetServerRequest{Id: "x"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}

func TestHandler_MapsAlreadyExistsToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.AlreadyExists("dup")}}
	_, err := h.CreateServer(context.Background(),
		connect.NewRequest(&controlplanev1.CreateServerRequest{WorkspaceId: "w1", Name: "Edge"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Errorf("code = %v, want AlreadyExists", connect.CodeOf(err))
	}
}
