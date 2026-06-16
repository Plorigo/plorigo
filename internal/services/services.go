// Package services owns a project's services: a service is a deployable component (a
// pre-built image, a PUBLIC git repo, or a template) living in exactly one environment,
// owning its source, container port, visibility, env vars, and deployment history. A
// project is a SYSTEM made of one or more services, each with a different source.
//
// It is a PRIVILEGED module: every mutation is authorized via the neutral authz.Authorizer
// port (satisfied by the policy module) before it runs, and audited in the same
// transaction. A service is created under an environment, so the owning workspace and
// project are resolved through the parent environment and denormalized onto the row. Git
// source validation reuses the same GitHubClient port as the sources module; an OAuth
// token is opened through the SecretBox port to validate a connected repo, and is never
// returned or logged. CreateService can also enqueue the service's first deployment through
// the Enqueuer port (satisfied by the deployments module) — services never imports
// deployments or sources. See docs/architecture/deployment-engine.md and modules.md.
package services

import (
	"context"
	"time"
)

// Source kinds: an image service runs a pre-built image; a git service clones+builds a
// repo; a template service is a curated preset that resolves to an image (image_ref).
const (
	SourceImage    = "image"
	SourceGit      = "git"
	SourceTemplate = "template"
)

// How a git source is reached. A public source carries no connection; an oauth source
// resolves through the workspace's GitHub connection. ('app' is a later slice.)
const (
	accessOAuth  = "oauth"
	accessPublic = "public"
)

// Visibility: a public service is published on a host port and routed through Caddy; a
// private service is reachable only by sibling services in the same environment.
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// provider is the only Git provider supported in this slice.
const provider = "github"

// Service is a project's deployable component (the domain model, independent of DB and
// transport types). The source is folded onto the row and discriminated by SourceKind.
// GitHubLogin is the connected account for an oauth source (display); it is not stored on
// the row and is resolved for reads.
type Service struct {
	ID            string
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	Name          string
	Slug          string
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
	Visibility    string
	RouteURL      string
	GitHubLogin   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateInput is what the dashboard supplies to create a service. The source is exactly one
// of: image_ref (image), template_id+image_ref (template), repo_url (public git), or
// owner+repo (an OAuth-connected git repo). DeployNow enqueues the first deployment onto
// ServerID (required when DeployNow).
type CreateInput struct {
	EnvironmentID string
	Name          string
	SourceKind    string
	ImageRef      string
	TemplateID    string
	RepoURL       string
	Owner         string
	Repo          string
	Branch        string
	ContainerPort int32
	Visibility    string
	ServerID      string
	DeployNow     bool
}

// UpdateSourceInput reconnects/changes a service's source (and port); the source shape
// mirrors CreateInput.
type UpdateSourceInput struct {
	ID            string
	SourceKind    string
	ImageRef      string
	TemplateID    string
	RepoURL       string
	Owner         string
	Repo          string
	Branch        string
	ContainerPort int32
}

// Result is what CreateService returns: the new service and, when DeployNow enqueued one,
// its first deployment's id (empty otherwise).
type Result struct {
	Service      Service
	DeploymentID string
}

// DetectInput previews how a PUBLIC repo would build. Branch is optional (empty = default).
type DetectInput struct {
	RepoURL string
	Branch  string
}

// Detection is the build-plan preview DetectFramework returns — a projection of the plan from
// internal/builder (the same logic the agent runs at build time). Status is "detected"
// (runtime + dockerfile populated), "dockerfile" (the repo ships its own Dockerfile), or
// "unsupported" (NextSteps says what to do).
type Detection struct {
	Status         string
	Runtime        string
	RuntimeLabel   string
	PackageManager string
	NodeVersion    string
	BuildCommand   string
	StartCommand   string
	ContainerPort  int32
	Dockerfile     string
	NextSteps      string
}

// Servicer is the surface other code (the handler, internal/app, tests) depends on. (Named
// Servicer, not Service, because the domain entity is Service.)
type Servicer interface {
	CreateService(ctx context.Context, in CreateInput) (Result, error)
	GetService(ctx context.Context, id string) (Service, error)
	ListByEnvironment(ctx context.Context, environmentID string) ([]Service, error)
	ListByProject(ctx context.Context, projectID string) ([]Service, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Service, error)
	UpdateSource(ctx context.Context, in UpdateSourceInput) (Service, error)
	UpdateVisibility(ctx context.Context, id, visibility string) (Service, error)
	DeleteService(ctx context.Context, id string) error
	DetectFramework(ctx context.Context, in DetectInput) (Detection, error)
}
