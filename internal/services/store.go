package services

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
)

// ServiceWrite is the data to insert an image or template service (no git columns).
type ServiceWrite struct {
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	Name          string
	Slug          string
	SourceKind    string
	ImageRef      string
	TemplateID    string
	ContainerPort int32
	Visibility    string
}

// GitServiceWrite is the data to insert a git service. ConnectionID is empty for a public
// source (stored as NULL); SourceAccess records how it is reached.
type GitServiceWrite struct {
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	Name          string
	Slug          string
	SourceAccess  string
	ConnectionID  string
	Provider      string
	Owner         string
	Repo          string
	FullName      string
	Branch        string
	DefaultBranch string
	IsPrivate     bool
	HTMLURL       string
	ContainerPort int32
	Visibility    string
}

// SourceWrite is the data to update a service's source (any kind) and port.
type SourceWrite struct {
	ID            string
	SourceKind    string
	ImageRef      string
	TemplateID    string
	ConnectionID  string
	Provider      string
	Owner         string
	Repo          string
	FullName      string
	Branch        string
	DefaultBranch string
	IsPrivate     bool
	HTMLURL       string
	SourceAccess  string
	ContainerPort int32
}

// Store is the repository port the service needs. Implemented by postgres.go, faked in
// tests. Mutations take a database.Tx so they commit with the audit row (and, for
// CreateService with deploy_now, with the first deployment enqueued by the Enqueuer).
type Store interface {
	InsertService(ctx context.Context, tx database.Tx, s ServiceWrite) (Service, error)
	InsertGitService(ctx context.Context, tx database.Tx, s GitServiceWrite) (Service, error)
	// GetService returns a service by id. ok is false (nil error) when none exists.
	GetService(ctx context.Context, id string) (Service, bool, error)
	ListByEnvironment(ctx context.Context, environmentID string) ([]Service, error)
	ListByProject(ctx context.Context, projectID string) ([]Service, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Service, error)
	UpdateServiceSource(ctx context.Context, tx database.Tx, s SourceWrite) (Service, error)
	UpdateVisibility(ctx context.Context, tx database.Tx, id, visibility string) (Service, error)
	// DeleteService removes a service (cascading its deployments + env vars). ok is false
	// when no row matched.
	DeleteService(ctx context.Context, tx database.Tx, id string) (deletedID string, ok bool, err error)

	// WorkspaceAndProjectForEnvironment resolves a new service's owning workspace and
	// project through the environment's parent project, so CreateService authorizes and
	// denormalizes both. ok is false (nil error) when the environment does not exist.
	WorkspaceAndProjectForEnvironment(ctx context.Context, environmentID string) (workspaceID, projectID string, ok bool, err error)
	// WorkspaceAndProjectForService resolves an existing service's workspace and project
	// (both denormalized onto the row), for read/update/delete authorization.
	WorkspaceAndProjectForService(ctx context.Context, serviceID string) (workspaceID, projectID string, ok bool, err error)
	// WorkspaceForServer resolves a server's owning workspace (the cross-tenant guard for
	// deploy_now). ok is false (nil error) when the server does not exist.
	WorkspaceForServer(ctx context.Context, serverID string) (workspaceID string, ok bool, err error)
	// WorkspaceForProject resolves a project's owning workspace (ListByProject authz). ok is
	// false (nil error) when the project does not exist.
	WorkspaceForProject(ctx context.Context, projectID string) (workspaceID string, ok bool, err error)

	// GetConnection resolves the workspace's GitHub connection id + account login for an
	// OAuth git source (a sibling read of source_connections). ok is false when none.
	GetConnection(ctx context.Context, workspaceID string) (connectionID, githubLogin string, ok bool, err error)
	// GetConnectionToken returns the sealed OAuth token for validating a connected repo.
	GetConnectionToken(ctx context.Context, workspaceID string) (ciphertext []byte, ok bool, err error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared here as a
// port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what services needs from the audit module.
// *audit.Service satisfies it structurally — services never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// SecretBox is the CONSUMER-DEFINED port for opening a sealed OAuth token to validate a
// connected repo. *crypto.Box satisfies it structurally. The plaintext stays in memory for
// the provider call only and is never returned or logged.
type SecretBox interface {
	Open(sealed []byte) ([]byte, error)
}

// GitHubClient is the CONSUMER-DEFINED port for validating a repo + branch. *github.Client
// satisfies it structurally (the same concrete client the sources module uses). It is the
// minimal slice services needs — repo discovery + OAuth live in the sources module.
type GitHubClient interface {
	GetRepository(ctx context.Context, token, owner, repo string) (github.RepoInfo, error)
	GetBranch(ctx context.Context, token, owner, repo, branch string) error
	// GetFileContent reads a single repo file at ref for framework detection; ok is false when
	// the file is absent. token is empty for a public repo.
	GetFileContent(ctx context.Context, token, owner, repo, ref, path string) (data []byte, ok bool, err error)
}

// Enqueuer is the CONSUMER-DEFINED port for queuing a new service's first deployment inside
// the create transaction. *deployments.Service satisfies it structurally — services never
// imports deployments. Returns the new deployment id.
type Enqueuer interface {
	EnqueueFirstDeployment(ctx context.Context, tx database.Tx, serviceID, serverID string) (string, error)
}

// EnvVarSetter is the CONSUMER-DEFINED port for writing a managed service's generated
// configuration (e.g. a database's credentials) inside the create transaction. *envvars.Service
// satisfies it structurally — services never imports envvars, and env_vars stays owned by that
// module. The caller has already authorized the service create, so this performs no auth of its
// own (it is part of the same provisioning action).
type EnvVarSetter interface {
	SetWithinTx(ctx context.Context, tx database.Tx, serviceID string, vars map[string]string) error
}
