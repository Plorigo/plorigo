package domains

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

type handler struct {
	svc Service
}

var _ controlplanev1connect.DomainServiceHandler = (*handler)(nil)

func (h *handler) CreateDomain(ctx context.Context, req *connect.Request[controlplanev1.CreateDomainRequest]) (*connect.Response[controlplanev1.CreateDomainResponse], error) {
	d, err := h.svc.CreateDomain(ctx, CreateInput{ServiceID: req.Msg.GetServiceId(), Hostname: req.Msg.GetHostname()})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateDomainResponse{Domain: toProto(d)}), nil
}

func (h *handler) ListDomainsByService(ctx context.Context, req *connect.Request[controlplanev1.ListDomainsByServiceRequest]) (*connect.Response[controlplanev1.ListDomainsByServiceResponse], error) {
	rows, err := h.svc.ListByService(ctx, req.Msg.GetServiceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Domain, 0, len(rows))
	for _, d := range rows {
		out = append(out, toProto(d))
	}
	return connect.NewResponse(&controlplanev1.ListDomainsByServiceResponse{Domains: out}), nil
}

func (h *handler) ListDomainsByProject(ctx context.Context, req *connect.Request[controlplanev1.ListDomainsByProjectRequest]) (*connect.Response[controlplanev1.ListDomainsByProjectResponse], error) {
	rows, err := h.svc.ListByProject(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Domain, 0, len(rows))
	for _, d := range rows {
		out = append(out, toProto(d))
	}
	return connect.NewResponse(&controlplanev1.ListDomainsByProjectResponse{Domains: out}), nil
}

func (h *handler) ListDomainsByWorkspace(ctx context.Context, req *connect.Request[controlplanev1.ListDomainsByWorkspaceRequest]) (*connect.Response[controlplanev1.ListDomainsByWorkspaceResponse], error) {
	rows, err := h.svc.ListByWorkspace(ctx, req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Domain, 0, len(rows))
	for _, d := range rows {
		out = append(out, toProto(d))
	}
	return connect.NewResponse(&controlplanev1.ListDomainsByWorkspaceResponse{Domains: out}), nil
}

func (h *handler) VerifyDomain(ctx context.Context, req *connect.Request[controlplanev1.VerifyDomainRequest]) (*connect.Response[controlplanev1.VerifyDomainResponse], error) {
	d, err := h.svc.VerifyDomain(ctx, req.Msg.GetId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.VerifyDomainResponse{Domain: toProto(d)}), nil
}

func (h *handler) DeleteDomain(ctx context.Context, req *connect.Request[controlplanev1.DeleteDomainRequest]) (*connect.Response[controlplanev1.DeleteDomainResponse], error) {
	if err := h.svc.DeleteDomain(ctx, req.Msg.GetId()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DeleteDomainResponse{}), nil
}

func toProto(d Domain) *controlplanev1.Domain {
	return &controlplanev1.Domain{
		Id:             d.ID,
		ServiceId:      d.ServiceID,
		EnvironmentId:  d.EnvironmentID,
		ProjectId:      d.ProjectID,
		WorkspaceId:    d.WorkspaceID,
		Hostname:       d.Hostname,
		Status:         d.Status,
		StatusMessage:  d.StatusMessage,
		DnsRecordType:  d.DNSRecordType,
		DnsRecordName:  d.DNSRecordName,
		DnsRecordValue: d.DNSRecordValue,
		CreatedAt:      d.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      d.UpdatedAt.UTC().Format(time.RFC3339),
		LastCheckedAt:  formatTimePtr(d.LastCheckedAt),
	}
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
