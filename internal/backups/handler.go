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

func (h *adminHandler) RestoreBackup(ctx context.Context, req *connect.Request[controlplanev1.RestoreBackupRequest]) (*connect.Response[controlplanev1.RestoreBackupResponse], error) {
	r, err := h.svc.RestoreBackup(ctx, req.Msg.GetBackupId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.RestoreBackupResponse{Restore: restoreToProto(r)}), nil
}

func (h *adminHandler) ListRestoreJobsByService(ctx context.Context, req *connect.Request[controlplanev1.ListRestoreJobsByServiceRequest]) (*connect.Response[controlplanev1.ListRestoreJobsByServiceResponse], error) {
	rows, err := h.svc.ListRestoresByService(ctx, req.Msg.GetServiceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.RestoreJob, 0, len(rows))
	for _, r := range rows {
		out = append(out, restoreToProto(r))
	}
	return connect.NewResponse(&controlplanev1.ListRestoreJobsByServiceResponse{Restores: out}), nil
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

func (h *gatewayHandler) PollRestoreJob(ctx context.Context, req *connect.Request[agentv1.PollRestoreJobRequest]) (*connect.Response[agentv1.PollRestoreJobResponse], error) {
	claimed, err := h.svc.PollRestoreJob(ctx, PollInput{AgentID: req.Msg.GetAgentId(), Credential: req.Msg.GetCredential()})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.PollRestoreJobResponse{
		HasWork:     claimed.HasWork,
		RestoreId:   claimed.RestoreID,
		ServiceId:   claimed.ServiceID,
		Engine:      claimed.Engine,
		PgUser:      claimed.PgUser,
		PgPassword:  claimed.PgPassword,
		PgDatabase:  claimed.PgDatabase,
		ArtifactUri: claimed.ArtifactURI,
	}), nil
}

func (h *gatewayHandler) ReportRestoreJob(ctx context.Context, req *connect.Request[agentv1.ReportRestoreJobRequest]) (*connect.Response[agentv1.ReportRestoreJobResponse], error) {
	err := h.svc.ReportRestoreJob(ctx, ReportRestoreInput{
		AgentID:    req.Msg.GetAgentId(),
		Credential: req.Msg.GetCredential(),
		RestoreID:  req.Msg.GetRestoreId(),
		Status:     req.Msg.GetStatus(),
		Message:    req.Msg.GetMessage(),
		Error:      req.Msg.GetError(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.ReportRestoreJobResponse{}), nil
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

func restoreToProto(r RestoreJob) *controlplanev1.RestoreJob {
	return &controlplanev1.RestoreJob{
		Id:            r.ID,
		BackupId:      r.BackupID,
		ServiceId:     r.ServiceID,
		EnvironmentId: r.EnvironmentID,
		ProjectId:     r.ProjectID,
		WorkspaceId:   r.WorkspaceID,
		ServerId:      r.ServerID,
		Status:        r.Status,
		Message:       r.Message,
		Error:         r.Error,
		CreatedAt:     r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
