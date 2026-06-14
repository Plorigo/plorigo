package deployments

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

// adminHandler adapts the dashboard-facing controlplane.v1.DeploymentService to the
// domain Service. It maps proto <-> domain and domain errors -> connect codes; no
// business logic here.
type adminHandler struct {
	svc Service
}

var _ controlplanev1connect.DeploymentServiceHandler = (*adminHandler)(nil)

func (h *adminHandler) CreateDeployment(ctx context.Context, req *connect.Request[controlplanev1.CreateDeploymentRequest]) (*connect.Response[controlplanev1.CreateDeploymentResponse], error) {
	dep, err := h.svc.Create(ctx, CreateInput{
		EnvironmentID: req.Msg.GetEnvironmentId(),
		ServerID:      req.Msg.GetServerId(),
		ImageRef:      req.Msg.GetImageRef(),
		ContainerPort: req.Msg.GetContainerPort(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateDeploymentResponse{Deployment: toProto(dep)}), nil
}

func (h *adminHandler) CreateDeploymentFromSource(ctx context.Context, req *connect.Request[controlplanev1.CreateDeploymentFromSourceRequest]) (*connect.Response[controlplanev1.CreateDeploymentFromSourceResponse], error) {
	dep, err := h.svc.CreateFromSource(ctx, CreateFromSourceInput{
		EnvironmentID: req.Msg.GetEnvironmentId(),
		ServerID:      req.Msg.GetServerId(),
		ContainerPort: req.Msg.GetContainerPort(),
		GitRef:        req.Msg.GetGitRef(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateDeploymentFromSourceResponse{Deployment: toProto(dep)}), nil
}

func (h *adminHandler) GetDeployment(ctx context.Context, req *connect.Request[controlplanev1.GetDeploymentRequest]) (*connect.Response[controlplanev1.GetDeploymentResponse], error) {
	dep, err := h.svc.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetDeploymentResponse{Deployment: toProto(dep)}), nil
}

func (h *adminHandler) ListDeploymentsByEnvironment(ctx context.Context, req *connect.Request[controlplanev1.ListDeploymentsByEnvironmentRequest]) (*connect.Response[controlplanev1.ListDeploymentsByEnvironmentResponse], error) {
	deps, err := h.svc.ListByEnvironment(ctx, req.Msg.GetEnvironmentId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListDeploymentsByEnvironmentResponse{Deployments: toProtos(deps)}), nil
}

func (h *adminHandler) ListDeploymentsByProject(ctx context.Context, req *connect.Request[controlplanev1.ListDeploymentsByProjectRequest]) (*connect.Response[controlplanev1.ListDeploymentsByProjectResponse], error) {
	deps, err := h.svc.ListByProject(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListDeploymentsByProjectResponse{Deployments: toProtos(deps)}), nil
}

func (h *adminHandler) ListDeploymentsByWorkspace(ctx context.Context, req *connect.Request[controlplanev1.ListDeploymentsByWorkspaceRequest]) (*connect.Response[controlplanev1.ListDeploymentsByWorkspaceResponse], error) {
	deps, err := h.svc.ListByWorkspace(ctx, req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListDeploymentsByWorkspaceResponse{Deployments: toProtos(deps)}), nil
}

func (h *adminHandler) ListDeploymentEvents(ctx context.Context, req *connect.Request[controlplanev1.ListDeploymentEventsRequest]) (*connect.Response[controlplanev1.ListDeploymentEventsResponse], error) {
	events, err := h.svc.ListEvents(ctx, req.Msg.GetDeploymentId(), req.Msg.GetAfterSeq())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.DeploymentEvent, 0, len(events))
	for _, e := range events {
		out = append(out, eventToProto(e))
	}
	return connect.NewResponse(&controlplanev1.ListDeploymentEventsResponse{Events: out}), nil
}

// gatewayHandler adapts the agent-facing agent.v1.DeployService to the domain Service.
// Its procedures are public at the auth interceptor; the service validates the agent
// credential carried in the request body and scopes work to the agent's own server.
type gatewayHandler struct {
	svc Service
}

var _ agentv1connect.DeployServiceHandler = (*gatewayHandler)(nil)

func (h *gatewayHandler) PollDeployment(ctx context.Context, req *connect.Request[agentv1.PollDeploymentRequest]) (*connect.Response[agentv1.PollDeploymentResponse], error) {
	claimed, err := h.svc.PollDeployment(ctx, PollInput{
		AgentID:    req.Msg.GetAgentId(),
		Credential: req.Msg.GetCredential(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.PollDeploymentResponse{
		HasWork:       claimed.HasWork,
		DeploymentId:  claimed.DeploymentID,
		ImageRef:      claimed.ImageRef,
		ContainerPort: claimed.ContainerPort,
		Env:           claimed.Env,
		AppLabel:      claimed.AppLabel,
		SourceKind:    claimed.SourceKind,
		CloneUrl:      claimed.CloneURL,
		GitRef:        claimed.GitRef,
		BuiltImageTag: claimed.BuiltImageTag,
	}), nil
}

func (h *gatewayHandler) ReportDeployment(ctx context.Context, req *connect.Request[agentv1.ReportDeploymentRequest]) (*connect.Response[agentv1.ReportDeploymentResponse], error) {
	if err := h.svc.ReportDeployment(ctx, ReportInput{
		AgentID:       req.Msg.GetAgentId(),
		Credential:    req.Msg.GetCredential(),
		DeploymentID:  req.Msg.GetDeploymentId(),
		Status:        req.Msg.GetStatus(),
		HostPort:      req.Msg.GetHostPort(),
		ContainerID:   req.Msg.GetContainerId(),
		LogLines:      req.Msg.GetLogLines(),
		Message:       req.Msg.GetMessage(),
		CommitSha:     req.Msg.GetCommitSha(),
		BuiltImageRef: req.Msg.GetBuiltImageRef(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.ReportDeploymentResponse{}), nil
}

func toProtos(ds []Deployment) []*controlplanev1.Deployment {
	out := make([]*controlplanev1.Deployment, 0, len(ds))
	for _, d := range ds {
		out = append(out, toProto(d))
	}
	return out
}

func toProto(d Deployment) *controlplanev1.Deployment {
	return &controlplanev1.Deployment{
		Id:            d.ID,
		EnvironmentId: d.EnvironmentID,
		ProjectId:     d.ProjectID,
		WorkspaceId:   d.WorkspaceID,
		ServerId:      d.ServerID,
		ImageRef:      d.ImageRef,
		ContainerPort: d.ContainerPort,
		HostPort:      d.HostPort,
		Status:        d.Status,
		Message:       d.Message,
		CreatedAt:     d.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     d.UpdatedAt.UTC().Format(time.RFC3339),
		SourceKind:    d.SourceKind,
		SourceAccess:  d.SourceAccess,
		CloneUrl:      d.CloneURL,
		GitRef:        d.GitRef,
		CommitSha:     d.CommitSha,
		BuiltImageRef: d.BuiltImageRef,
	}
}

func eventToProto(e Event) *controlplanev1.DeploymentEvent {
	return &controlplanev1.DeploymentEvent{
		Id:           e.ID,
		DeploymentId: e.DeploymentID,
		Seq:          e.Seq,
		Kind:         e.Kind,
		Status:       e.Status,
		Message:      e.Message,
		CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339),
	}
}
