package environments

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC EnvironmentService to the domain Service: it maps
// proto <-> domain and domain errors -> connect codes. No business logic lives here.
type handler struct {
	svc Service
}

var _ controlplanev1connect.EnvironmentServiceHandler = (*handler)(nil)

func (h *handler) CreateEnvironment(ctx context.Context, req *connect.Request[controlplanev1.CreateEnvironmentRequest]) (*connect.Response[controlplanev1.CreateEnvironmentResponse], error) {
	e, err := h.svc.Create(ctx, CreateInput{
		ProjectID: req.Msg.GetProjectId(),
		Name:      req.Msg.GetName(),
		Type:      req.Msg.GetType(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateEnvironmentResponse{Environment: toProto(e)}), nil
}

func (h *handler) GetEnvironment(ctx context.Context, req *connect.Request[controlplanev1.GetEnvironmentRequest]) (*connect.Response[controlplanev1.GetEnvironmentResponse], error) {
	e, err := h.svc.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetEnvironmentResponse{Environment: toProto(e)}), nil
}

func (h *handler) ListEnvironmentsByProject(ctx context.Context, req *connect.Request[controlplanev1.ListEnvironmentsByProjectRequest]) (*connect.Response[controlplanev1.ListEnvironmentsByProjectResponse], error) {
	es, err := h.svc.ListByProject(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Environment, 0, len(es))
	for _, e := range es {
		out = append(out, toProto(e))
	}
	return connect.NewResponse(&controlplanev1.ListEnvironmentsByProjectResponse{Environments: out}), nil
}

func toProto(e Environment) *controlplanev1.Environment {
	return &controlplanev1.Environment{
		Id:        e.ID,
		ProjectId: e.ProjectID,
		Name:      e.Name,
		Slug:      e.Slug,
		Type:      e.Type,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
}
