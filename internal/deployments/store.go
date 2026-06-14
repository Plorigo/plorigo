package deployments

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port the service needs. Implemented by postgres.go, faked in
// tests. Mutations that must commit with an audit row (or with a sibling write) take a
// database.Tx.
type Store interface {
	// WorkspaceAndProjectForEnvironment resolves a deployment target's owning workspace
	// and project through the environment's parent project, so this module authorizes
	// and denormalizes both without importing environments/projects. ok is false (nil
	// error) when the environment does not exist.
	WorkspaceAndProjectForEnvironment(ctx context.Context, environmentID string) (workspaceID, projectID string, ok bool, err error)
	// WorkspaceForServer resolves a server's owning workspace (cross-tenant guard at
	// create time). ok is false (nil error) when the server does not exist.
	WorkspaceForServer(ctx context.Context, serverID string) (workspaceID string, ok bool, err error)
	// WorkspaceForProject resolves a project's owning workspace (ListByProject authz).
	// ok is false (nil error) when the project does not exist.
	WorkspaceForProject(ctx context.Context, projectID string) (workspaceID string, ok bool, err error)
	// AgentServerByCredential resolves the agent and its server from a durable agent
	// credential hash, so the agent-facing RPCs authenticate the caller and scope work
	// to its own server. ok is false (nil error) when no agent has that credential.
	AgentServerByCredential(ctx context.Context, credentialHash []byte) (agentID, serverID string, ok bool, err error)
	// EnvVarsForEnvironment returns the environment's non-secret config to inject into
	// the container (reuses the env_vars table).
	EnvVarsForEnvironment(ctx context.Context, environmentID string) (map[string]string, error)
	// SourceForProject resolves a project's connected repository (clone URL + access) for
	// a git deployment, reading the project_sources table directly (a sibling-table read
	// modules.md Rule 2 permits from postgres.go). ok is false (nil error) when the
	// project has no connected source.
	SourceForProject(ctx context.Context, projectID string) (Source, bool, error)

	InsertDeployment(ctx context.Context, tx database.Tx, d NewDeployment) (Deployment, error)
	InsertDeploymentFromGit(ctx context.Context, tx database.Tx, d NewDeploymentFromGit) (Deployment, error)
	GetDeployment(ctx context.Context, deploymentID string) (Deployment, bool, error)
	ListByEnvironment(ctx context.Context, environmentID string) ([]Deployment, error)
	ListByProject(ctx context.Context, projectID string) ([]Deployment, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Deployment, error)
	ListEvents(ctx context.Context, deploymentID string, afterSeq int64) ([]Event, error)

	// ClaimNextForServer atomically claims the oldest queued deployment for a server
	// (status -> assigned). ok is false (nil error) when there is no queued work.
	ClaimNextForServer(ctx context.Context, tx database.Tx, serverID string) (Deployment, bool, error)
	// UpdateStatus records a status transition; a zero host port / empty container id
	// never clobbers a value already set.
	UpdateStatus(ctx context.Context, tx database.Tx, u StatusUpdate) error
	// SupersedePreviousRunning marks the environment's prior running deployment on this
	// server as superseded once a newer one reaches running.
	SupersedePreviousRunning(ctx context.Context, tx database.Tx, environmentID, serverID, deploymentID string) error
	AppendEvent(ctx context.Context, tx database.Tx, e NewEvent) error
}

// NewDeployment is the data to insert a queued image deployment.
type NewDeployment struct {
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	ServerID      string
	ImageRef      string
	ContainerPort int32
}

// NewDeploymentFromGit is the data to insert a queued git (build-from-source) deployment.
type NewDeploymentFromGit struct {
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	ServerID      string
	ContainerPort int32
	SourceAccess  string
	CloneURL      string
	GitRef        string
}

// Source is a project's connected repository, resolved for a git deployment.
type Source struct {
	CloneURL      string
	Branch        string
	DefaultBranch string
	Access        string // "public" | "oauth" | "app"
}

// StatusUpdate is an agent's reported transition for a deployment. A zero host port /
// empty container id / commit / built image / route_url never clobbers a value already set.
type StatusUpdate struct {
	DeploymentID  string
	Status        string
	Message       string
	HostPort      int32
	ContainerID   string
	CommitSha     string
	BuiltImageRef string
	RouteURL      string
}

// NewEvent is one timeline entry to append. Stream is set for KindLog events
// (StreamBuild or StreamRuntime) and left empty for status events.
type NewEvent struct {
	DeploymentID string
	Kind         string
	Status       string
	Message      string
	Stream       string
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared here
// as a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what deployments needs from the audit
// module. *audit.Service satisfies it structurally — deployments never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
