package sources

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
)

// fakeService records inputs and returns canned values. The token never appears on this
// surface: no method accepts or returns one (OAuth happens via the HTTP handlers).
type fakeService struct {
	status      ConnectionStatus
	repos       []Repository
	branches    []string
	err         error
	gotDisconnW string
}

func (f *fakeService) BeginGitHubAuth(context.Context, BeginAuthInput) (BeginAuthResult, error) {
	return BeginAuthResult{}, f.err
}
func (f *fakeService) CompleteGitHubAuth(context.Context, CompleteAuthInput) (CompleteAuthResult, error) {
	return CompleteAuthResult{}, f.err
}
func (f *fakeService) GetConnection(context.Context, string) (ConnectionStatus, error) {
	return f.status, f.err
}
func (f *fakeService) DisconnectGitHub(_ context.Context, workspaceID string) error {
	f.gotDisconnW = workspaceID
	return f.err
}
func (f *fakeService) ListRepositories(context.Context, ListReposInput) ([]Repository, error) {
	return f.repos, f.err
}
func (f *fakeService) ListBranches(context.Context, string, string, string) ([]string, error) {
	return f.branches, f.err
}

func TestHandler_GetConnection(t *testing.T) {
	h := &handler{svc: &fakeService{status: ConnectionStatus{Configured: true, Connected: true, Connection: Connection{GitHubLogin: "octocat", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}}
	resp, err := h.GetConnection(context.Background(), connect.NewRequest(&controlplanev1.GetConnectionRequest{WorkspaceId: "w1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Msg.GetConfigured() || !resp.Msg.GetConnected() {
		t.Fatalf("flags = configured:%v connected:%v", resp.Msg.GetConfigured(), resp.Msg.GetConnected())
	}
	if resp.Msg.GetConnection().GetGithubLogin() != "octocat" {
		t.Errorf("github_login = %q", resp.Msg.GetConnection().GetGithubLogin())
	}
}

func TestHandler_GetConnection_NotConnectedOmitsConnection(t *testing.T) {
	h := &handler{svc: &fakeService{status: ConnectionStatus{Configured: true, Connected: false}}}
	resp, err := h.GetConnection(context.Background(), connect.NewRequest(&controlplanev1.GetConnectionRequest{WorkspaceId: "w1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.GetConnection() != nil {
		t.Error("connection should be nil when not connected")
	}
}

func TestHandler_ListRepositories(t *testing.T) {
	h := &handler{svc: &fakeService{repos: []Repository{{FullName: "o/a"}, {FullName: "o/b"}}}}
	resp, err := h.ListRepositories(context.Background(), connect.NewRequest(&controlplanev1.ListRepositoriesRequest{WorkspaceId: "w1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(resp.Msg.GetRepositories()); n != 2 {
		t.Fatalf("len = %d, want 2", n)
	}
}

func TestHandler_ListBranches(t *testing.T) {
	h := &handler{svc: &fakeService{branches: []string{"main", "dev"}}}
	resp, err := h.ListBranches(context.Background(), connect.NewRequest(&controlplanev1.ListBranchesRequest{WorkspaceId: "w1", Owner: "o", Repo: "r"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(resp.Msg.GetBranches()); n != 2 {
		t.Fatalf("len = %d, want 2", n)
	}
}

func TestHandler_DisconnectGitHub(t *testing.T) {
	svc := &fakeService{}
	h := &handler{svc: svc}
	if _, err := h.DisconnectGitHub(context.Background(), connect.NewRequest(&controlplanev1.DisconnectGitHubRequest{WorkspaceId: "w1"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.gotDisconnW != "w1" {
		t.Errorf("workspace = %q, want w1", svc.gotDisconnW)
	}
}

func TestHandler_MapsDomainErrorToConnectCode(t *testing.T) {
	h := &handler{svc: &fakeService{err: problem.NotFound("nope")}}
	_, err := h.GetConnection(context.Background(), connect.NewRequest(&controlplanev1.GetConnectionRequest{WorkspaceId: "w1"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}
