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
// surface: no method accepts or returns one (OAuth happens via the HTTP handlers), and
// the domain read types carry no token field.
type fakeService struct {
	status      ConnectionStatus
	repos       []Repository
	branches    []string
	source      Source
	list        []Source
	err         error
	gotProject  string
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
func (f *fakeService) ConnectRepository(_ context.Context, in ConnectRepoInput) (Source, error) {
	f.gotProject = in.ProjectID
	return f.source, f.err
}
func (f *fakeService) GetProjectSource(context.Context, string) (Source, error) {
	return f.source, f.err
}
func (f *fakeService) ListByWorkspace(context.Context, string) ([]Source, error) {
	return f.list, f.err
}
func (f *fakeService) DisconnectRepository(_ context.Context, projectID string) error {
	f.gotProject = projectID
	return f.err
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

func TestHandler_ConnectRepository(t *testing.T) {
	svc := &fakeService{source: Source{ID: "s1", ProjectID: "p1", FullName: "o/r", Branch: "main", CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	h := &handler{svc: svc}
	resp, err := h.ConnectRepository(context.Background(), connect.NewRequest(&controlplanev1.ConnectRepositoryRequest{ProjectId: "p1", Owner: "o", Repo: "r", Branch: "main"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.GetSource().GetFullName() != "o/r" {
		t.Errorf("full_name = %q", resp.Msg.GetSource().GetFullName())
	}
	if svc.gotProject != "p1" {
		t.Errorf("project = %q, want p1", svc.gotProject)
	}
}

func TestHandler_GetProjectSource(t *testing.T) {
	h := &handler{svc: &fakeService{source: Source{ID: "s1", FullName: "o/r", CreatedAt: time.Now(), UpdatedAt: time.Now()}}}
	resp, err := h.GetProjectSource(context.Background(), connect.NewRequest(&controlplanev1.GetProjectSourceRequest{ProjectId: "p1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Msg.GetSource().GetFullName() != "o/r" {
		t.Errorf("full_name = %q", resp.Msg.GetSource().GetFullName())
	}
}

func TestHandler_ListSourcesByWorkspace(t *testing.T) {
	h := &handler{svc: &fakeService{list: []Source{{ID: "s1"}, {ID: "s2"}}}}
	resp, err := h.ListSourcesByWorkspace(context.Background(), connect.NewRequest(&controlplanev1.ListSourcesByWorkspaceRequest{WorkspaceId: "w1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := len(resp.Msg.GetSources()); n != 2 {
		t.Fatalf("len = %d, want 2", n)
	}
}

func TestHandler_DisconnectRepository(t *testing.T) {
	svc := &fakeService{}
	h := &handler{svc: svc}
	if _, err := h.DisconnectRepository(context.Background(), connect.NewRequest(&controlplanev1.DisconnectRepositoryRequest{ProjectId: "p1"})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.gotProject != "p1" {
		t.Errorf("project = %q, want p1", svc.gotProject)
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
	_, err := h.GetProjectSource(context.Background(), connect.NewRequest(&controlplanev1.GetProjectSourceRequest{ProjectId: "p1"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("code = %v, want NotFound", connect.CodeOf(err))
	}
}
