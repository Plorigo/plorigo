package readiness

import (
	"context"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

type handler struct {
	svc Service
}

var _ controlplanev1connect.ReadinessServiceHandler = (*handler)(nil)

func (h *handler) GetServiceReadiness(ctx context.Context, req *connect.Request[controlplanev1.GetServiceReadinessRequest]) (*connect.Response[controlplanev1.GetServiceReadinessResponse], error) {
	list, err := h.svc.ServiceReadiness(ctx, req.Msg.GetServiceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetServiceReadinessResponse{Checklist: toProto(list)}), nil
}

func (h *handler) GetEnvironmentReadiness(ctx context.Context, req *connect.Request[controlplanev1.GetEnvironmentReadinessRequest]) (*connect.Response[controlplanev1.GetEnvironmentReadinessResponse], error) {
	list, err := h.svc.EnvironmentReadiness(ctx, req.Msg.GetEnvironmentId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetEnvironmentReadinessResponse{Checklist: toProto(list)}), nil
}

// toProto maps a domain checklist to the wire type. The enum-like fields are plain strings on the
// wire (see readiness.proto), so the mapping is a direct string conversion.
func toProto(list Checklist) *controlplanev1.ReadinessChecklist {
	checks := make([]*controlplanev1.ReadinessCheck, 0, len(list.Checks))
	for _, c := range list.Checks {
		checks = append(checks, &controlplanev1.ReadinessCheck{
			Category:    c.Category,
			Severity:    string(c.Severity),
			State:       string(c.State),
			Title:       c.Title,
			Detail:      c.Detail,
			Remediation: c.Remediation,
		})
	}
	return &controlplanev1.ReadinessChecklist{
		OverallLevel: string(list.OverallLevel),
		Checks:       checks,
	}
}
