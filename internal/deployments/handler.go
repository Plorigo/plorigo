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

func (h *adminHandler) CreateDeploymentForService(ctx context.Context, req *connect.Request[controlplanev1.CreateDeploymentForServiceRequest]) (*connect.Response[controlplanev1.CreateDeploymentForServiceResponse], error) {
	dep, err := h.svc.CreateForService(ctx, CreateForServiceInput{
		ServiceID:     req.Msg.GetServiceId(),
		ServerID:      req.Msg.GetServerId(),
		ContainerPort: req.Msg.GetContainerPort(),
		GitRef:        req.Msg.GetGitRef(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateDeploymentForServiceResponse{Deployment: toProto(dep)}), nil
}

func (h *adminHandler) CreatePreviewDeployment(ctx context.Context, req *connect.Request[controlplanev1.CreatePreviewDeploymentRequest]) (*connect.Response[controlplanev1.CreatePreviewDeploymentResponse], error) {
	dep, err := h.svc.CreatePreview(ctx, CreatePreviewInput{
		ServiceID:     req.Msg.GetServiceId(),
		ServerID:      req.Msg.GetServerId(),
		Branch:        req.Msg.GetBranch(),
		PRNumber:      req.Msg.GetPrNumber(),
		ContainerPort: req.Msg.GetContainerPort(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreatePreviewDeploymentResponse{Deployment: toProto(dep)}), nil
}

func (h *adminHandler) TeardownPreview(ctx context.Context, req *connect.Request[controlplanev1.TeardownPreviewRequest]) (*connect.Response[controlplanev1.TeardownPreviewResponse], error) {
	t, err := h.svc.TeardownPreview(ctx, req.Msg.GetDeploymentId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.TeardownPreviewResponse{Teardown: teardownToProto(t)}), nil
}

func (h *adminHandler) ListTeardownJobsByService(ctx context.Context, req *connect.Request[controlplanev1.ListTeardownJobsByServiceRequest]) (*connect.Response[controlplanev1.ListTeardownJobsByServiceResponse], error) {
	rows, err := h.svc.ListTeardownsByService(ctx, req.Msg.GetServiceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.TeardownJob, 0, len(rows))
	for _, t := range rows {
		out = append(out, teardownToProto(t))
	}
	return connect.NewResponse(&controlplanev1.ListTeardownJobsByServiceResponse{Teardowns: out}), nil
}

func (h *adminHandler) RollbackDeployment(ctx context.Context, req *connect.Request[controlplanev1.RollbackDeploymentRequest]) (*connect.Response[controlplanev1.RollbackDeploymentResponse], error) {
	dep, err := h.svc.RollbackToDeployment(ctx, req.Msg.GetTargetDeploymentId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.RollbackDeploymentResponse{Deployment: toProto(dep)}), nil
}

func (h *adminHandler) ListDeploymentsByService(ctx context.Context, req *connect.Request[controlplanev1.ListDeploymentsByServiceRequest]) (*connect.Response[controlplanev1.ListDeploymentsByServiceResponse], error) {
	deps, err := h.svc.ListByService(ctx, req.Msg.GetServiceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListDeploymentsByServiceResponse{Deployments: toProtos(deps)}), nil
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
		Visibility:    claimed.Visibility,
		NetworkName:   claimed.NetworkName,
		NetworkAlias:  claimed.NetworkAlias,
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
		LogStream:     req.Msg.GetLogStream(),
		Message:       req.Msg.GetMessage(),
		CommitSha:     req.Msg.GetCommitSha(),
		BuiltImageRef: req.Msg.GetBuiltImageRef(),
		RouteURL:      req.Msg.GetRouteUrl(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.ReportDeploymentResponse{}), nil
}

func (h *gatewayHandler) SyncRoutes(ctx context.Context, req *connect.Request[agentv1.SyncRoutesRequest]) (*connect.Response[agentv1.SyncRoutesResponse], error) {
	routes := make([]ManagedRoute, 0, len(req.Msg.GetRoutes()))
	for _, r := range req.Msg.GetRoutes() {
		routes = append(routes, ManagedRoute{
			ServiceID:    r.GetServiceId(),
			DeploymentID: r.GetDeploymentId(),
			HostPort:     r.GetHostPort(),
		})
	}
	overrides, err := h.svc.SyncRoutes(ctx, SyncRoutesInput{
		AgentID:    req.Msg.GetAgentId(),
		Credential: req.Msg.GetCredential(),
		Routes:     routes,
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*agentv1.RouteOverride, 0, len(overrides))
	for _, o := range overrides {
		out = append(out, &agentv1.RouteOverride{ServiceId: o.ServiceID, Hostnames: o.Hostnames})
	}
	return connect.NewResponse(&agentv1.SyncRoutesResponse{Overrides: out}), nil
}

func (h *gatewayHandler) ReportRouteSync(ctx context.Context, req *connect.Request[agentv1.ReportRouteSyncRequest]) (*connect.Response[agentv1.ReportRouteSyncResponse], error) {
	results := make([]RouteSyncResult, 0, len(req.Msg.GetResults()))
	for _, r := range req.Msg.GetResults() {
		results = append(results, RouteSyncResult{
			ServiceID:    r.GetServiceId(),
			DeploymentID: r.GetDeploymentId(),
			Hostnames:    r.GetHostnames(),
			OK:           r.GetOk(),
			Message:      r.GetMessage(),
		})
	}
	if err := h.svc.ReportRouteSync(ctx, ReportRouteSyncInput{
		AgentID:    req.Msg.GetAgentId(),
		Credential: req.Msg.GetCredential(),
		Results:    results,
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.ReportRouteSyncResponse{}), nil
}

// teardownGatewayHandler serves the agent-facing agent.v1.TeardownService. Like the deploy
// gateway, its procedures are public at the auth interceptor; the service validates the agent
// credential carried in the request body and scopes work to the agent's own server.
type teardownGatewayHandler struct {
	svc Service
}

var _ agentv1connect.TeardownServiceHandler = (*teardownGatewayHandler)(nil)

func (h *teardownGatewayHandler) PollTeardownJob(ctx context.Context, req *connect.Request[agentv1.PollTeardownJobRequest]) (*connect.Response[agentv1.PollTeardownJobResponse], error) {
	claimed, err := h.svc.PollTeardownJob(ctx, PollInput{AgentID: req.Msg.GetAgentId(), Credential: req.Msg.GetCredential()})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.PollTeardownJobResponse{
		HasWork:     claimed.HasWork,
		TeardownId:  claimed.TeardownID,
		RouteKey:    claimed.RouteKey,
		NetworkName: claimed.NetworkName,
	}), nil
}

func (h *teardownGatewayHandler) ReportTeardownJob(ctx context.Context, req *connect.Request[agentv1.ReportTeardownJobRequest]) (*connect.Response[agentv1.ReportTeardownJobResponse], error) {
	if err := h.svc.ReportTeardownJob(ctx, ReportTeardownInput{
		AgentID:    req.Msg.GetAgentId(),
		Credential: req.Msg.GetCredential(),
		TeardownID: req.Msg.GetTeardownId(),
		Status:     req.Msg.GetStatus(),
		Message:    req.Msg.GetMessage(),
		Error:      req.Msg.GetError(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.ReportTeardownJobResponse{}), nil
}

func teardownToProto(t TeardownJob) *controlplanev1.TeardownJob {
	return &controlplanev1.TeardownJob{
		Id:            t.ID,
		DeploymentId:  t.DeploymentID,
		ServiceId:     t.ServiceID,
		RouteKey:      t.RouteKey,
		EnvironmentId: t.EnvironmentID,
		ProjectId:     t.ProjectID,
		WorkspaceId:   t.WorkspaceID,
		ServerId:      t.ServerID,
		Status:        t.Status,
		Message:       t.Message,
		Error:         t.Error,
		CreatedAt:     t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     t.UpdatedAt.UTC().Format(time.RFC3339),
	}
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
		Id:             d.ID,
		ServiceId:      d.ServiceID,
		EnvironmentId:  d.EnvironmentID,
		ProjectId:      d.ProjectID,
		WorkspaceId:    d.WorkspaceID,
		ServerId:       d.ServerID,
		ImageRef:       d.ImageRef,
		ContainerPort:  d.ContainerPort,
		HostPort:       d.HostPort,
		Status:         d.Status,
		Message:        d.Message,
		CreatedAt:      d.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      d.UpdatedAt.UTC().Format(time.RFC3339),
		SourceKind:     d.SourceKind,
		SourceAccess:   d.SourceAccess,
		CloneUrl:       d.CloneURL,
		GitRef:         d.GitRef,
		CommitSha:      d.CommitSha,
		BuiltImageRef:  d.BuiltImageRef,
		RouteUrl:       d.RouteURL,
		RolledBackFrom: d.RolledBackFrom,
		Kind:           d.Kind,
		PrNumber:       d.PRNumber,
		PrUrl:          d.PRURL,
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
		Stream:       e.Stream,
		CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339),
	}
}
