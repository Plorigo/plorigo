package projects

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// workspaceHandler adapts the ConnectRPC WorkspaceService to the same projects
// Service. Authorization and business logic live in the service; this only maps
// proto <-> domain and domain errors -> connect codes.
type workspaceHandler struct {
	svc Service
}

var _ controlplanev1connect.WorkspaceServiceHandler = (*workspaceHandler)(nil)

func (h *workspaceHandler) CreateWorkspace(ctx context.Context, req *connect.Request[controlplanev1.CreateWorkspaceRequest]) (*connect.Response[controlplanev1.CreateWorkspaceResponse], error) {
	ws, err := h.svc.CreateWorkspace(ctx, CreateWorkspaceInput{Name: req.Msg.GetName()})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateWorkspaceResponse{Workspace: workspaceToProto(ws)}), nil
}

func (h *workspaceHandler) ListMyWorkspaces(ctx context.Context, _ *connect.Request[controlplanev1.ListMyWorkspacesRequest]) (*connect.Response[controlplanev1.ListMyWorkspacesResponse], error) {
	p := principal.FromContext(ctx)
	if !p.IsAuthenticated() {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	wss, err := h.svc.ListMyWorkspaces(ctx, p.UserID)
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Workspace, 0, len(wss))
	for _, w := range wss {
		out = append(out, workspaceToProto(w))
	}
	return connect.NewResponse(&controlplanev1.ListMyWorkspacesResponse{Workspaces: out}), nil
}

func (h *workspaceHandler) InviteMember(ctx context.Context, req *connect.Request[controlplanev1.InviteMemberRequest]) (*connect.Response[controlplanev1.InviteMemberResponse], error) {
	if err := h.svc.InviteMember(ctx, InviteMemberInput{
		WorkspaceID: req.Msg.GetWorkspaceId(),
		Email:       req.Msg.GetEmail(),
		Role:        req.Msg.GetRole(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.InviteMemberResponse{}), nil
}

func (h *workspaceHandler) ListMembers(ctx context.Context, req *connect.Request[controlplanev1.ListMembersRequest]) (*connect.Response[controlplanev1.ListMembersResponse], error) {
	ms, err := h.svc.ListMembers(ctx, ListMembersInput{WorkspaceID: req.Msg.GetWorkspaceId()})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Member, 0, len(ms))
	for _, m := range ms {
		out = append(out, memberToProto(m))
	}
	return connect.NewResponse(&controlplanev1.ListMembersResponse{Members: out}), nil
}

func (h *workspaceHandler) ChangeMemberRole(ctx context.Context, req *connect.Request[controlplanev1.ChangeMemberRoleRequest]) (*connect.Response[controlplanev1.ChangeMemberRoleResponse], error) {
	if err := h.svc.ChangeMemberRole(ctx, ChangeRoleInput{
		WorkspaceID: req.Msg.GetWorkspaceId(),
		UserID:      req.Msg.GetUserId(),
		Role:        req.Msg.GetRole(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ChangeMemberRoleResponse{}), nil
}

func (h *workspaceHandler) RemoveMember(ctx context.Context, req *connect.Request[controlplanev1.RemoveMemberRequest]) (*connect.Response[controlplanev1.RemoveMemberResponse], error) {
	if err := h.svc.RemoveMember(ctx, RemoveMemberInput{
		WorkspaceID: req.Msg.GetWorkspaceId(),
		UserID:      req.Msg.GetUserId(),
	}); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.RemoveMemberResponse{}), nil
}

func workspaceToProto(w Workspace) *controlplanev1.Workspace {
	return &controlplanev1.Workspace{
		Id:        w.ID,
		Name:      w.Name,
		Slug:      w.Slug,
		CreatedAt: w.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func memberToProto(m Member) *controlplanev1.Member {
	return &controlplanev1.Member{
		UserId:    m.UserID,
		Email:     m.Email,
		Role:      m.Role,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
	}
}
