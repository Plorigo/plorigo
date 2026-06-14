// Package deployments is the control-plane side of the deploy loop: it records a
// deployment (an attempt to run a container image in an environment on a connected
// server), dispatches it to the server's agent, and tracks its status and timeline.
//
// It serves TWO ConnectRPC surfaces: controlplane.v1.DeploymentService (dashboard-
// facing, session/token-authenticated and policy-authorized) and agent.v1.DeployService
// (the agent gateway: the agent claims work and reports progress, authenticated by its
// durable agent credential — the same credential as Heartbeat — NOT a user session).
// See docs/architecture/deployment-engine.md and agent.md. This slice deploys a
// PRE-BUILT image or public Git Dockerfile build, then asks the agent to make it
// reachable through Caddy. SSL and cryptographic job signing are later slices.
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
	StatusCloning    = "cloning"    // the agent is cloning the source repo (git)
	StatusBuilding   = "building"   // the agent is building the image (git)
	StatusPulling    = "pulling"    // the agent is pulling the image (image)
	StatusStarting   = "starting"   // the agent is creating/starting the container
	StatusRouting    = "routing"    // the agent is validating/reloading Caddy routing
	StatusRunning    = "running"    // the container is up and passed its health check
	StatusFailed     = "failed"     // the attempt failed (see message / logs)
	StatusSuperseded = "superseded" // replaced by a newer running deployment
)

// Source kinds: an image deployment runs a pre-built image; a git deployment clones a
// repo and builds its Dockerfile on the server first.
const (
	SourceImage = "image"
	SourceGit   = "git"
)

// Deployment event kinds.
const (
	KindStatus = "status" // a status transition
	KindLog    = "log"    // a log line (see the stream constants below)
)

// Log streams a KindLog event can belong to. StreamBuild is the agent's own
// clone/build/pull/start output; StreamRuntime is the container's stdout/stderr. The
// empty string is used for status events and for legacy log rows recorded before
// streams were distinguished.
const (
	StreamBuild   = "build"
	StreamRuntime = "runtime"
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

	// Build-from-Git. SourceKind is SourceImage or SourceGit; the rest are set only for
	// git deployments. CommitSha and BuiltImageRef are filled by the agent after build.
	SourceKind    string
	SourceAccess  string
	CloneURL      string
	GitRef        string
	CommitSha     string
	BuiltImageRef string

	// RouteURL is the real deployment URL (e.g. http://{env-id}.localhost:8083) computed
	// by the agent and stored so the dashboard can display a clickable link.
	RouteURL string
	// CustomDomain is an optional user-supplied domain that the agent adds as an
	// additional Caddy route alongside the auto-generated {env-id}.{base-domain}.
	// Empty string means no custom domain is configured.
	CustomDomain string
}

// Event is one entry in a deployment's timeline: a status transition (KindStatus) or
// a log line (KindLog). Seq is monotonic per deployment for incremental fetch. For a
// KindLog event, Stream is StreamBuild or StreamRuntime; it is empty for status events
// and for legacy rows recorded before streams were distinguished.
type Event struct {
	ID           string
	DeploymentID string
	Seq          int64
	Kind         string
	Status       string
	Message      string
	Stream       string
	CreatedAt    time.Time
}

// CreateInput is what the dashboard supplies to trigger a pre-built-image deployment.
type CreateInput struct {
	EnvironmentID string
	ServerID      string
	ImageRef      string
	ContainerPort int32
}

// CreateFromSourceInput is what the dashboard supplies to build-and-deploy the project's
// connected repository. It carries no repo URL — the service resolves the project's
// source server-side. GitRef is optional (empty = the source's default branch).
type CreateFromSourceInput struct {
	EnvironmentID string
	ServerID      string
	ContainerPort int32
	GitRef        string
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

	// CustomDomain is an optional custom domain the user attached to this deployment.
	// The agent adds it as an additional Caddy route alongside the generated subdomain.
	CustomDomain string
	// Build-from-Git. For a git deployment SourceKind is SourceGit and the agent clones
	// CloneURL at GitRef, builds the Dockerfile to BuiltImageTag, then runs that tag. No
	// credential is included: this slice builds public repositories only.
	SourceKind    string
	CloneURL      string
	GitRef        string
	BuiltImageTag string
}

// ReportInput is an agent's progress update for a deployment it is executing.
type ReportInput struct {
	AgentID       string
	Credential    string
	DeploymentID  string
	Status        string
	HostPort      int32
	ContainerID   string
	LogLines      []string
	LogStream     string // which stream LogLines belong to: StreamBuild or StreamRuntime
	Message       string
	CommitSha     string // the exact commit built (git deployments)
	BuiltImageRef string // the local image tag the agent built (git deployments)
	RouteURL      string // the real deployment URL computed by the agent
}

// Service is the surface other code depends on. It backs both the dashboard-facing
// controlplane.v1.DeploymentService and the agent-facing agent.v1.DeployService.
type Service interface {
	Create(ctx context.Context, in CreateInput) (Deployment, error)
	CreateFromSource(ctx context.Context, in CreateFromSourceInput) (Deployment, error)
	Get(ctx context.Context, deploymentID string) (Deployment, error)
	ListByEnvironment(ctx context.Context, environmentID string) ([]Deployment, error)
	ListByProject(ctx context.Context, projectID string) ([]Deployment, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Deployment, error)
	ListEvents(ctx context.Context, deploymentID string, afterSeq int64) ([]Event, error)
	SetCustomDomain(ctx context.Context, deploymentID, customDomain string) (Deployment, error)

	// Agent gateway (credential-authenticated, NOT policy-authorized — like Heartbeat).
	PollDeployment(ctx context.Context, in PollInput) (Claimed, error)
	ReportDeployment(ctx context.Context, in ReportInput) error
}
