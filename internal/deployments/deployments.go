// Package deployments is the control-plane side of the deploy loop: it records a
// deployment (an attempt to run a container image in an environment on a connected
// server), dispatches it to the server's agent, and tracks its status and timeline.
//
// It serves TWO ConnectRPC surfaces: controlplane.v1.DeploymentService (dashboard-
// facing, session/token-authenticated and policy-authorized) and agent.v1.DeployService
// (the agent gateway: the agent claims work and reports progress, authenticated by its
// durable agent credential — the same credential as Heartbeat — NOT a user session).
// See docs/architecture/deployment-engine.md and agent.md. This first slice deploys a
// PRE-BUILT image reachable on a published host port; build-from-Git, Caddy routing,
// SSL, and cryptographic job signing are later slices.
package deployments

import (
	"context"
	"time"
)

// Deployment statuses, persisted on the deployments row and CHECK-constrained in the
// migration as defense-in-depth.
const (
	StatusQueued     = "queued"     // recorded by the control plane, not yet claimed
	StatusAssigned   = "assigned"   // claimed by the server's agent
	StatusPulling    = "pulling"    // the agent is pulling the image
	StatusStarting   = "starting"   // the agent is creating/starting the container
	StatusRunning    = "running"    // the container is up and passed its health check
	StatusFailed     = "failed"     // the attempt failed (see message / logs)
	StatusSuperseded = "superseded" // replaced by a newer running deployment
)

// Deployment event kinds.
const (
	KindStatus = "status" // a status transition
	KindLog    = "log"    // a runtime log line
)

// Deployment is one attempt to run an image in an environment on a server. The
// workspace and project are denormalized from the environment (both immutable).
type Deployment struct {
	ID            string
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	ServerID      string
	ImageRef      string
	ContainerPort int32
	HostPort      int32
	ContainerID   string
	Status        string
	Message       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Event is one entry in a deployment's timeline: a status transition (KindStatus) or
// a runtime log line (KindLog). Seq is monotonic per deployment for incremental fetch.
type Event struct {
	ID           string
	DeploymentID string
	Seq          int64
	Kind         string
	Status       string
	Message      string
	CreatedAt    time.Time
}

// CreateInput is what the dashboard supplies to trigger a deployment.
type CreateInput struct {
	EnvironmentID string
	ServerID      string
	ImageRef      string
	ContainerPort int32
}

// PollInput is what an agent presents to claim the next queued deployment for its
// server. The credential is the durable agent credential (validated by its hash).
type PollInput struct {
	AgentID    string
	Credential string
}

// Claimed is the job handed to an agent when it polls, including the environment's
// configured (non-secret) env vars to inject and the app label used to find and
// replace the previous container on a redeploy.
type Claimed struct {
	HasWork       bool
	DeploymentID  string
	ImageRef      string
	ContainerPort int32
	Env           map[string]string
	AppLabel      string
}

// ReportInput is an agent's progress update for a deployment it is executing.
type ReportInput struct {
	AgentID      string
	Credential   string
	DeploymentID string
	Status       string
	HostPort     int32
	ContainerID  string
	LogLines     []string
	Message      string
}

// Service is the surface other code depends on. It backs both the dashboard-facing
// controlplane.v1.DeploymentService and the agent-facing agent.v1.DeployService.
type Service interface {
	Create(ctx context.Context, in CreateInput) (Deployment, error)
	Get(ctx context.Context, deploymentID string) (Deployment, error)
	ListByEnvironment(ctx context.Context, environmentID string) ([]Deployment, error)
	ListByProject(ctx context.Context, projectID string) ([]Deployment, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Deployment, error)
	ListEvents(ctx context.Context, deploymentID string, afterSeq int64) ([]Event, error)

	// Agent gateway (credential-authenticated, NOT policy-authorized — like Heartbeat).
	PollDeployment(ctx context.Context, in PollInput) (Claimed, error)
	ReportDeployment(ctx context.Context, in ReportInput) error
}
