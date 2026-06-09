package servers

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC ServerService to the domain Service: it maps proto <->
// domain and domain errors -> connect codes. No business logic lives here.
type handler struct {
	svc Service
}

var _ controlplanev1connect.ServerServiceHandler = (*handler)(nil)

func (h *handler) CreateServer(ctx context.Context, req *connect.Request[controlplanev1.CreateServerRequest]) (*connect.Response[controlplanev1.CreateServerResponse], error) {
	srv, err := h.svc.Create(ctx, CreateInput{
		WorkspaceID: req.Msg.GetWorkspaceId(),
		Name:        req.Msg.GetName(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateServerResponse{Server: toProto(srv)}), nil
}

func (h *handler) GetServer(ctx context.Context, req *connect.Request[controlplanev1.GetServerRequest]) (*connect.Response[controlplanev1.GetServerResponse], error) {
	srv, err := h.svc.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetServerResponse{Server: toProto(srv)}), nil
}

func (h *handler) ListServersByWorkspace(ctx context.Context, req *connect.Request[controlplanev1.ListServersByWorkspaceRequest]) (*connect.Response[controlplanev1.ListServersByWorkspaceResponse], error) {
	srvs, err := h.svc.ListByWorkspace(ctx, req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Server, 0, len(srvs))
	for _, srv := range srvs {
		out = append(out, toProto(srv))
	}
	return connect.NewResponse(&controlplanev1.ListServersByWorkspaceResponse{Servers: out}), nil
}

func (h *handler) DeleteServer(ctx context.Context, req *connect.Request[controlplanev1.DeleteServerRequest]) (*connect.Response[controlplanev1.DeleteServerResponse], error) {
	if err := h.svc.Delete(ctx, req.Msg.GetId()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DeleteServerResponse{}), nil
}

func toProto(s Server) *controlplanev1.Server {
	return &controlplanev1.Server{
		Id:          s.ID,
		WorkspaceId: s.WorkspaceID,
		Name:        s.Name,
		Slug:        s.Slug,
		CreatedAt:   s.CreatedAt.UTC().Format(time.RFC3339),
	}
}
