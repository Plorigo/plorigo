package services

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC ServiceService to the domain Servicer: it maps proto <->
// domain and domain errors -> connect codes. No business logic lives here.
type handler struct {
	svc Servicer
}

var _ controlplanev1connect.ServiceServiceHandler = (*handler)(nil)

func (h *handler) CreateService(ctx context.Context, req *connect.Request[controlplanev1.CreateServiceRequest]) (*connect.Response[controlplanev1.CreateServiceResponse], error) {
	res, err := h.svc.CreateService(ctx, CreateInput{
		EnvironmentID: req.Msg.GetEnvironmentId(),
		Name:          req.Msg.GetName(),
		SourceKind:    req.Msg.GetSourceKind(),
		ImageRef:      req.Msg.GetImageRef(),
		TemplateID:    req.Msg.GetTemplateId(),
		RepoURL:       req.Msg.GetRepoUrl(),
		Owner:         req.Msg.GetOwner(),
		Repo:          req.Msg.GetRepo(),
		Branch:        req.Msg.GetBranch(),
		ContainerPort: req.Msg.GetContainerPort(),
		Visibility:    req.Msg.GetVisibility(),
		ServerID:      req.Msg.GetServerId(),
		DeployNow:     req.Msg.GetDeployNow(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateServiceResponse{
		Service:      toProto(res.Service),
		DeploymentId: res.DeploymentID,
	}), nil
}

func (h *handler) GetService(ctx context.Context, req *connect.Request[controlplanev1.GetServiceRequest]) (*connect.Response[controlplanev1.GetServiceResponse], error) {
	svc, err := h.svc.GetService(ctx, req.Msg.GetId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.GetServiceResponse{Service: toProto(svc)}), nil
}

func (h *handler) ListServicesByEnvironment(ctx context.Context, req *connect.Request[controlplanev1.ListServicesByEnvironmentRequest]) (*connect.Response[controlplanev1.ListServicesByEnvironmentResponse], error) {
	svcs, err := h.svc.ListByEnvironment(ctx, req.Msg.GetEnvironmentId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListServicesByEnvironmentResponse{Services: toProtos(svcs)}), nil
}

func (h *handler) ListServicesByProject(ctx context.Context, req *connect.Request[controlplanev1.ListServicesByProjectRequest]) (*connect.Response[controlplanev1.ListServicesByProjectResponse], error) {
	svcs, err := h.svc.ListByProject(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListServicesByProjectResponse{Services: toProtos(svcs)}), nil
}

func (h *handler) ListServicesByWorkspace(ctx context.Context, req *connect.Request[controlplanev1.ListServicesByWorkspaceRequest]) (*connect.Response[controlplanev1.ListServicesByWorkspaceResponse], error) {
	svcs, err := h.svc.ListByWorkspace(ctx, req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListServicesByWorkspaceResponse{Services: toProtos(svcs)}), nil
}

func (h *handler) UpdateServiceSource(ctx context.Context, req *connect.Request[controlplanev1.UpdateServiceSourceRequest]) (*connect.Response[controlplanev1.UpdateServiceSourceResponse], error) {
	svc, err := h.svc.UpdateSource(ctx, UpdateSourceInput{
		ID:            req.Msg.GetId(),
		SourceKind:    req.Msg.GetSourceKind(),
		ImageRef:      req.Msg.GetImageRef(),
		TemplateID:    req.Msg.GetTemplateId(),
		RepoURL:       req.Msg.GetRepoUrl(),
		Owner:         req.Msg.GetOwner(),
		Repo:          req.Msg.GetRepo(),
		Branch:        req.Msg.GetBranch(),
		ContainerPort: req.Msg.GetContainerPort(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.UpdateServiceSourceResponse{Service: toProto(svc)}), nil
}

func (h *handler) UpdateServiceVisibility(ctx context.Context, req *connect.Request[controlplanev1.UpdateServiceVisibilityRequest]) (*connect.Response[controlplanev1.UpdateServiceVisibilityResponse], error) {
	svc, err := h.svc.UpdateVisibility(ctx, req.Msg.GetId(), req.Msg.GetVisibility())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.UpdateServiceVisibilityResponse{Service: toProto(svc)}), nil
}

func (h *handler) DeleteService(ctx context.Context, req *connect.Request[controlplanev1.DeleteServiceRequest]) (*connect.Response[controlplanev1.DeleteServiceResponse], error) {
	if err := h.svc.DeleteService(ctx, req.Msg.GetId()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DeleteServiceResponse{}), nil
}

func (h *handler) DetectFramework(ctx context.Context, req *connect.Request[controlplanev1.DetectFrameworkRequest]) (*connect.Response[controlplanev1.DetectFrameworkResponse], error) {
	d, err := h.svc.DetectFramework(ctx, DetectInput{
		RepoURL: req.Msg.GetRepoUrl(),
		Branch:  req.Msg.GetBranch(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DetectFrameworkResponse{
		Status:         d.Status,
		Runtime:        d.Runtime,
		RuntimeLabel:   d.RuntimeLabel,
		PackageManager: d.PackageManager,
		NodeVersion:    d.NodeVersion,
		BuildCommand:   d.BuildCommand,
		StartCommand:   d.StartCommand,
		ContainerPort:  d.ContainerPort,
		Dockerfile:     d.Dockerfile,
		NextSteps:      d.NextSteps,
	}), nil
}

func toProtos(svcs []Service) []*controlplanev1.Service {
	out := make([]*controlplanev1.Service, 0, len(svcs))
	for _, s := range svcs {
		out = append(out, toProto(s))
	}
	return out
}

func toProto(s Service) *controlplanev1.Service {
	return &controlplanev1.Service{
		Id:            s.ID,
		EnvironmentId: s.EnvironmentID,
		ProjectId:     s.ProjectID,
		WorkspaceId:   s.WorkspaceID,
		Name:          s.Name,
		Slug:          s.Slug,
		SourceKind:    s.SourceKind,
		ImageRef:      s.ImageRef,
		TemplateId:    s.TemplateID,
		ConnectionId:  s.ConnectionID,
		Provider:      s.Provider,
		Owner:         s.Owner,
		Repo:          s.Repo,
		FullName:      s.FullName,
		Branch:        s.Branch,
		DefaultBranch: s.DefaultBranch,
		IsPrivate:     s.IsPrivate,
		HtmlUrl:       s.HTMLURL,
		SourceAccess:  s.SourceAccess,
		ContainerPort: s.ContainerPort,
		Visibility:    s.Visibility,
		RouteUrl:      s.RouteURL,
		// The internal hostname siblings use is the service's slug (its network alias).
		InternalHost: s.Slug,
		GithubLogin:  s.GitHubLogin,
		CreatedAt:    s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
