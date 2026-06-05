package projects

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC ProjectService to the domain Service: it maps
// proto <-> domain and domain errors -> connect codes. No business logic lives here.
type handler struct {
	svc Service
}

var _ controlplanev1connect.ProjectServiceHandler = (*handler)(nil)

func (h *handler) CreateProject(ctx context.Context, req *connect.Request[controlplanev1.CreateProjectRequest]) (*connect.Response[controlplanev1.CreateProjectResponse], error) {
	p, err := h.svc.Create(ctx, CreateInput{
		WorkspaceID: req.Msg.GetWorkspaceId(),
		Name:        req.Msg.GetName(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateProjectResponse{Project: toProto(p)}), nil
}

func (h *handler) GetProject(ctx context.Context, req *connect.Request[controlplanev1.GetProjectRequest]) (*connect.Response[controlplanev1.GetProjectResponse], error) {
	p, err := h.svc.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetProjectResponse{Project: toProto(p)}), nil
}

func (h *handler) ListProjectsByWorkspace(ctx context.Context, req *connect.Request[controlplanev1.ListProjectsByWorkspaceRequest]) (*connect.Response[controlplanev1.ListProjectsByWorkspaceResponse], error) {
	ps, err := h.svc.ListByWorkspace(ctx, req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Project, 0, len(ps))
	for _, p := range ps {
		out = append(out, toProto(p))
	}
	return connect.NewResponse(&controlplanev1.ListProjectsByWorkspaceResponse{Projects: out}), nil
}

func toProto(p Project) *controlplanev1.Project {
	return &controlplanev1.Project{
		Id:          p.ID,
		WorkspaceId: p.WorkspaceID,
		Name:        p.Name,
		Slug:        p.Slug,
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
	}
}
