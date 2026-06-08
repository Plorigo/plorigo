package envvars

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC EnvVarService to the domain Service: it maps proto
// <-> domain and domain errors -> connect codes. No business logic lives here.
type handler struct {
	svc Service
}

var _ controlplanev1connect.EnvVarServiceHandler = (*handler)(nil)

func (h *handler) SetEnvVar(ctx context.Context, req *connect.Request[controlplanev1.SetEnvVarRequest]) (*connect.Response[controlplanev1.SetEnvVarResponse], error) {
	ev, err := h.svc.Set(ctx, SetInput{
		EnvironmentID: req.Msg.GetEnvironmentId(),
		Key:           req.Msg.GetKey(),
		Value:         req.Msg.GetValue(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.SetEnvVarResponse{EnvVar: toProto(ev)}), nil
}

func (h *handler) ListEnvVars(ctx context.Context, req *connect.Request[controlplanev1.ListEnvVarsRequest]) (*connect.Response[controlplanev1.ListEnvVarsResponse], error) {
	evs, err := h.svc.List(ctx, req.Msg.GetEnvironmentId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.EnvVar, 0, len(evs))
	for _, ev := range evs {
		out = append(out, toProto(ev))
	}
	return connect.NewResponse(&controlplanev1.ListEnvVarsResponse{EnvVars: out}), nil
}

func (h *handler) DeleteEnvVar(ctx context.Context, req *connect.Request[controlplanev1.DeleteEnvVarRequest]) (*connect.Response[controlplanev1.DeleteEnvVarResponse], error) {
	if err := h.svc.Delete(ctx, DeleteInput{
		EnvironmentID: req.Msg.GetEnvironmentId(),
		Key:           req.Msg.GetKey(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DeleteEnvVarResponse{}), nil
}

func toProto(e EnvVar) *controlplanev1.EnvVar {
	return &controlplanev1.EnvVar{
		Id:            e.ID,
		EnvironmentId: e.EnvironmentID,
		Key:           e.Key,
		Value:         e.Value,
		CreatedAt:     e.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     e.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
