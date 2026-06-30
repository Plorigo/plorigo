package sources

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC SourceService to the domain Service: it maps proto <->
// domain and domain errors -> connect codes. No business logic lives here. No mapping
// ever carries the access token — the domain read types do not have one.
type handler struct {
	svc Service
}

var _ controlplanev1connect.SourceServiceHandler = (*handler)(nil)

func (h *handler) GetConnection(ctx context.Context, req *connect.Request[controlplanev1.GetConnectionRequest]) (*connect.Response[controlplanev1.GetConnectionResponse], error) {
	st, err := h.svc.GetConnection(ctx, req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	resp := &controlplanev1.GetConnectionResponse{
		Configured:    st.Configured,
		Connected:     st.Connected,
		AppConfigured: st.AppConfigured,
		AppConnected:  st.AppConnected,
	}
	if st.Connected {
		resp.Connection = toProtoConnection(st.Connection)
	}
	return connect.NewResponse(resp), nil
}

func (h *handler) ListRepositories(ctx context.Context, req *connect.Request[controlplanev1.ListRepositoriesRequest]) (*connect.Response[controlplanev1.ListRepositoriesResponse], error) {
	repos, err := h.svc.ListRepositories(ctx, ListReposInput{
		WorkspaceID: req.Msg.GetWorkspaceId(),
		Query:       req.Msg.GetQuery(),
		Page:        int(req.Msg.GetPage()),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	out := make([]*controlplanev1.Repository, 0, len(repos))
	for _, r := range repos {
		out = append(out, toProtoRepository(r))
	}
	return connect.NewResponse(&controlplanev1.ListRepositoriesResponse{Repositories: out}), nil
}

func (h *handler) ListBranches(ctx context.Context, req *connect.Request[controlplanev1.ListBranchesRequest]) (*connect.Response[controlplanev1.ListBranchesResponse], error) {
	branches, err := h.svc.ListBranches(ctx, req.Msg.GetWorkspaceId(), req.Msg.GetOwner(), req.Msg.GetRepo())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListBranchesResponse{Branches: branches}), nil
}

func (h *handler) DisconnectGitHub(ctx context.Context, req *connect.Request[controlplanev1.DisconnectGitHubRequest]) (*connect.Response[controlplanev1.DisconnectGitHubResponse], error) {
	if err := h.svc.DisconnectGitHub(ctx, req.Msg.GetWorkspaceId()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DisconnectGitHubResponse{}), nil
}

func toProtoConnection(c Connection) *controlplanev1.Connection {
	connectedBy := ""
	if c.ConnectedBy != nil {
		connectedBy = *c.ConnectedBy
	}
	return &controlplanev1.Connection{
		WorkspaceId: c.WorkspaceID,
		Provider:    c.Provider,
		GithubLogin: c.GitHubLogin,
		Scopes:      c.Scopes,
		ConnectedBy: connectedBy,
		CreatedAt:   c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toProtoRepository(r Repository) *controlplanev1.Repository {
	return &controlplanev1.Repository{
		Owner:         r.Owner,
		Name:          r.Name,
		FullName:      r.FullName,
		DefaultBranch: r.DefaultBranch,
		IsPrivate:     r.IsPrivate,
		HtmlUrl:       r.HTMLURL,
		Description:   r.Description,
	}
}
