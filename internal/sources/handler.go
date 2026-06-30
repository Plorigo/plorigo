package sources

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// handler adapts the ConnectRPC SourceService to the domain Service: it maps proto <-> domain and
// domain errors -> connect codes. No business logic here, and no mapping ever carries a token — the
// domain read types do not have one.
type handler struct {
	svc Service
}

var _ controlplanev1connect.SourceServiceHandler = (*handler)(nil)

func (h *handler) ListConnections(ctx context.Context, req *connect.Request[controlplanev1.ListConnectionsRequest]) (*connect.Response[controlplanev1.ListConnectionsResponse], error) {
	res, err := h.svc.ListConnections(ctx, req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	conns := make([]*controlplanev1.Connection, 0, len(res.Connections))
	for _, c := range res.Connections {
		conns = append(conns, toProtoConnection(c))
	}
	provs := make([]*controlplanev1.ProviderStatus, 0, len(res.Providers))
	for _, p := range res.Providers {
		provs = append(provs, &controlplanev1.ProviderStatus{
			Provider:        p.Provider,
			DisplayName:     p.DisplayName,
			OauthConfigured: p.OAuthConfigured,
			AppConfigured:   p.AppConfigured,
			Available:       p.Available,
		})
	}
	return connect.NewResponse(&controlplanev1.ListConnectionsResponse{Connections: conns, Providers: provs}), nil
}

func (h *handler) ListRepositories(ctx context.Context, req *connect.Request[controlplanev1.ListRepositoriesRequest]) (*connect.Response[controlplanev1.ListRepositoriesResponse], error) {
	repos, err := h.svc.ListRepositories(ctx, ListReposInput{
		ConnectionID: req.Msg.GetConnectionId(),
		Query:        req.Msg.GetQuery(),
		Page:         int(req.Msg.GetPage()),
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
	branches, err := h.svc.ListBranches(ctx, req.Msg.GetConnectionId(), req.Msg.GetOwner(), req.Msg.GetRepo())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.ListBranchesResponse{Branches: branches}), nil
}

func (h *handler) DisconnectConnection(ctx context.Context, req *connect.Request[controlplanev1.DisconnectConnectionRequest]) (*connect.Response[controlplanev1.DisconnectConnectionResponse], error) {
	if err := h.svc.DisconnectConnection(ctx, req.Msg.GetConnectionId()); err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.DisconnectConnectionResponse{}), nil
}

func toProtoConnection(c Connection) *controlplanev1.Connection {
	connectedBy := ""
	if c.ConnectedBy != nil {
		connectedBy = *c.ConnectedBy
	}
	var accountID int64
	if c.AccountID != nil {
		accountID = *c.AccountID
	}
	installationID := ""
	if c.InstallationID != nil {
		installationID = *c.InstallationID
	}
	return &controlplanev1.Connection{
		Id:             c.ID,
		WorkspaceId:    c.WorkspaceID,
		Provider:       c.Provider,
		Kind:           c.Kind,
		AccountLogin:   c.AccountLogin,
		AccountId:      accountID,
		InstallationId: installationID,
		Scopes:         c.Scopes,
		ConnectedBy:    connectedBy,
		CreatedAt:      c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      c.UpdatedAt.UTC().Format(time.RFC3339),
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
