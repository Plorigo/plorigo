package backups

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// adminHandler serves the dashboard-facing controlplane.v1.BackupService.
type adminHandler struct {
	svc Service
}

var _ controlplanev1connect.BackupServiceHandler = (*adminHandler)(nil)

func (h *adminHandler) CreateBackup(ctx context.Context, req *connect.Request[controlplanev1.CreateBackupRequest]) (*connect.Response[controlplanev1.CreateBackupResponse], error) {
	b, err := h.svc.CreateBackup(ctx, req.Msg.GetServiceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateBackupResponse{Backup: toProto(b)}), nil
}

func (h *adminHandler) GetBackup(ctx context.Context, req *connect.Request[controlplanev1.GetBackupRequest]) (*connect.Response[controlplanev1.GetBackupResponse], error) {
	b, err := h.svc.GetBackup(ctx, req.Msg.GetId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetBackupResponse{Backup: toProto(b)}), nil
}

func (h *adminHandler) ListBackupsByService(ctx context.Context, req *connect.Request[controlplanev1.ListBackupsByServiceRequest]) (*connect.Response[controlplanev1.ListBackupsByServiceResponse], error) {
	rows, err := h.svc.ListByService(ctx, req.Msg.GetServiceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Backup, 0, len(rows))
	for _, b := range rows {
		out = append(out, toProto(b))
	}
	return connect.NewResponse(&controlplanev1.ListBackupsByServiceResponse{Backups: out}), nil
}

// gatewayHandler serves the agent-facing agent.v1.BackupService. Its procedures are public at the
// auth interceptor; the service validates the agent credential carried in the request body.
type gatewayHandler struct {
	svc Service
}

var _ agentv1connect.BackupServiceHandler = (*gatewayHandler)(nil)

func (h *gatewayHandler) PollBackupJob(ctx context.Context, req *connect.Request[agentv1.PollBackupJobRequest]) (*connect.Response[agentv1.PollBackupJobResponse], error) {
	claimed, err := h.svc.PollBackupJob(ctx, PollInput{AgentID: req.Msg.GetAgentId(), Credential: req.Msg.GetCredential()})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.PollBackupJobResponse{
		HasWork:    claimed.HasWork,
		BackupId:   claimed.BackupID,
		Kind:       claimed.Kind,
		ServiceId:  claimed.ServiceID,
		Engine:     claimed.Engine,
		PgUser:     claimed.PgUser,
		PgPassword: claimed.PgPassword,
		PgDatabase: claimed.PgDatabase,
	}), nil
}

func (h *gatewayHandler) ReportBackupJob(ctx context.Context, req *connect.Request[agentv1.ReportBackupJobRequest]) (*connect.Response[agentv1.ReportBackupJobResponse], error) {
	err := h.svc.ReportBackupJob(ctx, ReportInput{
		AgentID:     req.Msg.GetAgentId(),
		Credential:  req.Msg.GetCredential(),
		BackupID:    req.Msg.GetBackupId(),
		Status:      req.Msg.GetStatus(),
		ArtifactURI: req.Msg.GetArtifactUri(),
		SizeBytes:   req.Msg.GetSizeBytes(),
		Checksum:    req.Msg.GetChecksum(),
		Message:     req.Msg.GetMessage(),
		Error:       req.Msg.GetError(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.ReportBackupJobResponse{}), nil
}

func toProto(b Backup) *controlplanev1.Backup {
	return &controlplanev1.Backup{
		Id:            b.ID,
		ServiceId:     b.ServiceID,
		EnvironmentId: b.EnvironmentID,
		ProjectId:     b.ProjectID,
		WorkspaceId:   b.WorkspaceID,
		ServerId:      b.ServerID,
		Destination:   b.Destination,
		ArtifactUri:   b.ArtifactURI,
		SizeBytes:     b.SizeBytes,
		Checksum:      b.Checksum,
		Status:        b.Status,
		Message:       b.Message,
		Error:         b.Error,
		CreatedAt:     b.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     b.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
