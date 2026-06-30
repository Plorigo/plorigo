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

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Deployment statuses, persisted on the deployments row and CHECK-constrained in the
// migration as defense-in-depth.
const (
	StatusQueued      = "queued"      // recorded by the control plane, not yet claimed
	StatusAssigned    = "assigned"    // claimed by the server's agent
	StatusCloning     = "cloning"     // the agent is cloning the source repo (git)
	StatusBuilding    = "building"    // the agent is building the image (git)
	StatusPulling     = "pulling"     // the agent is pulling the image (image)
	StatusStarting    = "starting"    // the agent is creating/starting the container
	StatusHealthcheck = "healthcheck" // the agent is probing the new container's health
	StatusRouting     = "routing"     // the agent is validating/reloading Caddy routing
	StatusRunning     = "running"     // the container is up and passed its health check
	StatusFailed      = "failed"      // the attempt failed (see message / logs)
	StatusSuperseded  = "superseded"  // replaced by a newer running deployment
)

// Source kinds: an image deployment runs a pre-built image; a git deployment clones a
// repo and builds its Dockerfile on the server first.
const (
	SourceImage = "image"
	SourceGit   = "git"
)

// Deployment kinds. A production deployment is the service's main release (route_key = the
// service id). A preview deployment is a build of a branch or pull-request head ref that runs
// alongside production with its own route_key — its own Caddy route, container-replacement
// group, supersede scope, and isolated network — so it never disturbs production. See
// docs/architecture/deployment-engine.md.
const (
	KindProduction = "production"
	KindPreview    = "preview"
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

// Deployment is one attempt to run a service in an environment on a server. The service,
// environment, project, and workspace are all denormalized onto the row (all immutable),
// so authorization, scoping, and the dashboard's views need no joins.
type Deployment struct {
	ID            string
	ServiceID     string
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

	// RouteURL is the real deployment URL (e.g. http://{route-key}.localhost:8083) computed
	// by the agent for a PUBLIC service and stored so the dashboard can display a clickable
	// link. Empty for a private service (no public route).
	RouteURL string

	// RolledBackFrom is the id of the previous healthy deployment this one reproduces, set
	// when it was created by a rollback. Empty for a normal deploy.
	RolledBackFrom string

	// Preview fields. Kind is KindProduction or KindPreview. RouteKey is the service id for a
	// production deployment and a distinct, DNS-safe key per preview (it drives the Caddy route,
	// the container-replacement group, the supersede scope, and the isolated network). PRNumber
	// and PRURL link a pull-request preview back to its GitHub PR (0 / "" for a branch preview).
	Kind     string
	RouteKey string
	PRNumber int32
	PRURL    string

	// Optional basic-auth protection for a preview's URL. AuthHash is a bcrypt hash (never the
	// plaintext password); AuthUser the username. Empty for production and unprotected previews.
	AuthUser string
	AuthHash string
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

// CreateForServiceInput is what the dashboard supplies to (re)deploy an existing service.
// It carries no repo URL or image — the service resolves the service's source server-side.
// ContainerPort and GitRef are optional overrides (0 / empty = the service's configured
// port and branch/default).
type CreateForServiceInput struct {
	ServiceID     string
	ServerID      string
	ContainerPort int32
	GitRef        string
}

// CreatePreviewInput is what the dashboard supplies to create a PREVIEW deployment of an
// existing git service. Exactly one of Branch or PRNumber identifies what to build; a PRNumber
// is resolved to its head ref and the PR is linked back via the deployment's pr_url.
// ContainerPort is an optional override (0 = the service's configured port).
type CreatePreviewInput struct {
	ServiceID     string
	ServerID      string
	Branch        string
	PRNumber      int32
	ContainerPort int32
	// Optional basic-auth protection for the preview's URL. When Password is set the service
	// bcrypt-hashes it (the plaintext is never stored or sent to the agent); PasswordUser defaults
	// to "preview" when blank.
	Password     string
	PasswordUser string
}

// ServiceForDeploy is a service's source + routing facts, resolved when enqueuing a deploy.
// It is read from the services table (a sibling-table read modules.md Rule 2 permits).
type ServiceForDeploy struct {
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	SourceKind    string // "image" | "git" | "template"
	ImageRef      string
	SourceAccess  string // "" | "public" | "oauth" | "app"
	Owner         string
	Repo          string
	Branch        string
	DefaultBranch string
	ContainerPort int32
	Visibility    string // "public" | "private"
	Slug          string
}

// PollInput is what an agent presents to claim the next queued deployment for its
// server. The credential is the durable agent credential (validated by its hash).
type PollInput struct {
	AgentID    string
	Credential string
}

// Claimed is the job handed to an agent when it polls, including the service's configured
// (non-secret) env vars to inject and the app label used to find and replace the previous
// container on a redeploy.
type Claimed struct {
	HasWork       bool
	DeploymentID  string
	ImageRef      string
	ContainerPort int32
	Env           map[string]string
	// AppLabel is the SERVICE id: the agent stamps it on the container (to find and replace
	// this service's previous container) and uses it as the Caddy route host label, so two
	// services in one environment never collide.
	AppLabel string

	// Build-from-Git. For a git deployment SourceKind is SourceGit and the agent clones
	// CloneURL at GitRef, builds the Dockerfile to BuiltImageTag, then runs that tag. No
	// credential is included: this slice builds public repositories only.
	SourceKind    string
	CloneURL      string
	GitRef        string
	BuiltImageTag string

	// Visibility + internal networking. A public service is published + routed through Caddy;
	// a private service is reachable only by siblings. Every service joins NetworkName (one
	// per environment) with NetworkAlias (its slug) for sibling-to-sibling traffic.
	Visibility   string
	NetworkName  string
	NetworkAlias string

	// Optional basic-auth for a protected preview's Caddy route. BasicAuthHash is a bcrypt hash
	// (never plaintext); empty for production and unprotected previews.
	BasicAuthUser string
	BasicAuthHash string
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

// ManagedRoute is one public service route the agent is currently serving.
type ManagedRoute struct {
	ServiceID    string
	DeploymentID string
	HostPort     int32
}

// RouteOverride carries verified custom hostnames for a managed service route.
type RouteOverride struct {
	ServiceID string
	Hostnames []string
}

// SyncRoutesInput is the agent's request for custom hostnames to add to its Caddy routes.
type SyncRoutesInput struct {
	AgentID    string
	Credential string
	Routes     []ManagedRoute
}

// RouteSyncResult reports whether the agent activated or failed custom hostnames.
type RouteSyncResult struct {
	ServiceID    string
	DeploymentID string
	Hostnames    []string
	OK           bool
	Message      string
}

// ReportRouteSyncInput is the agent's Caddy route-sync result report.
type ReportRouteSyncInput struct {
	AgentID    string
	Credential string
	Results    []RouteSyncResult
}

// Service is the surface other code depends on. It backs both the dashboard-facing
// controlplane.v1.DeploymentService and the agent-facing agent.v1.DeployService.
type Service interface {
	CreateForService(ctx context.Context, in CreateForServiceInput) (Deployment, error)
	// CreatePreview enqueues a preview deployment of an existing git service — a build of a
	// branch or a pull request's head ref — that runs alongside production with its own
	// route_key (isolated URL + network) and never supersedes production. Public git services
	// only in this slice.
	CreatePreview(ctx context.Context, in CreatePreviewInput) (Deployment, error)
	// RollbackToDeployment enqueues a new deployment that reproduces a previous healthy
	// deployment's artifact (same image, or same repo pinned to the built commit) on the
	// same service and server, linked back via rolled_back_from. The target must be running
	// or superseded.
	RollbackToDeployment(ctx context.Context, targetDeploymentID string) (Deployment, error)
	Get(ctx context.Context, deploymentID string) (Deployment, error)
	ListByService(ctx context.Context, serviceID string) ([]Deployment, error)
	ListByEnvironment(ctx context.Context, environmentID string) ([]Deployment, error)
	ListByProject(ctx context.Context, projectID string) ([]Deployment, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Deployment, error)
	ListEvents(ctx context.Context, deploymentID string, afterSeq int64) ([]Event, error)

	// EnqueueFirstDeployment queues a brand new service's first deployment inside the
	// CALLER's transaction (the services module, which has already authorized the create),
	// resolving the service's source from the tx so create+deploy commit atomically. It is
	// a consumer-defined port: the services module declares the same signature and Go
	// structural typing satisfies it — services never imports deployments. Returns the new
	// deployment id.
	EnqueueFirstDeployment(ctx context.Context, tx database.Tx, serviceID, serverID string) (string, error)

	// TeardownPreview enqueues the removal of a preview deployment — stop + remove its container
	// and drop its Caddy route — on the preview's server. The target must be a preview deployment
	// (kind = "preview"); production deployments are never torn down this way. Idempotent.
	TeardownPreview(ctx context.Context, deploymentID string) (TeardownJob, error)
	ListTeardownsByService(ctx context.Context, serviceID string) ([]TeardownJob, error)

	// CreatePreviewForPR and TeardownPreviewForPR are the webhook-driven seam (phase 4):
	// create/refresh or remove a PR preview by service + PR number, resolving the server and the
	// preview internally. They are triggered by a VERIFIED GitHub webhook (no user session), so —
	// like EnqueueFirstDeployment — they are NOT policy-authorized; the webhook's signature check +
	// installation→workspace→service mapping is the gate. Public git only; TeardownPreviewForPR is
	// an idempotent no-op when no active preview exists.
	CreatePreviewForPR(ctx context.Context, serviceID string, prNumber int32) (deploymentID string, err error)
	TeardownPreviewForPR(ctx context.Context, serviceID string, prNumber int32) error

	// ExpirePreviews tears down every running preview deployment older than ttl, so abandoned
	// previews don't accumulate. It is the control-plane expiry sweep's entry point — not
	// policy-authorized (a system action), idempotent, and returns how many it enqueued. ttl <= 0
	// disables it (returns 0 without scanning).
	ExpirePreviews(ctx context.Context, ttl time.Duration) (int, error)

	// Agent gateway (credential-authenticated, NOT policy-authorized — like Heartbeat).
	PollDeployment(ctx context.Context, in PollInput) (Claimed, error)
	ReportDeployment(ctx context.Context, in ReportInput) error
	SyncRoutes(ctx context.Context, in SyncRoutesInput) ([]RouteOverride, error)
	ReportRouteSync(ctx context.Context, in ReportRouteSyncInput) error
	// Agent gateway for preview teardown (credential-authenticated).
	PollTeardownJob(ctx context.Context, in PollInput) (ClaimedTeardown, error)
	ReportTeardownJob(ctx context.Context, in ReportTeardownInput) error
}
