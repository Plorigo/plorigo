package readiness

import "context"

// This module owns no tables. Its "ports" are read-only views onto sibling modules, defined as
// small consumer-defined interfaces over NEUTRAL structs (below). internal/app adapts each
// sibling Service() to the matching port, so readiness imports no other module.

// ServiceFacts is the service-row subset readiness needs.
type ServiceFacts struct {
	ID            string
	Name          string
	WorkspaceID   string
	EnvironmentID string
	SourceKind    string // "git" | "image" | "template"
	Visibility    string // "public" | "private"
	RouteURL      string // generated public URL, empty until first deploy
	ContainerPort int32
}

// ConfigEntry is one configuration entry. Secrets never expose a value (Value is blank); only
// their presence (the key) is known, so placeholder detection runs on variables only.
type ConfigEntry struct {
	Key    string
	Secret bool
	Value  string
}

// DomainFact is a custom domain's verification/SSL state.
type DomainFact struct {
	Hostname string
	Status   string // "blocked" | "pending_dns" | "verified" | "active" | "failed"
}

// DeploymentFact is the latest deployment's outcome for a service.
type DeploymentFact struct {
	Status   string // deployments status: queued..running|failed|superseded
	ServerID string
}

// ServerReadiness is the best connected server's deployability for a workspace.
type ServerReadiness struct {
	HasServer bool
	State     string // "ready" | "degraded" | "blocked" | "unknown"
	Reason    string // plain-English reason (empty when ready)
}

// ServiceReader reads service facts. A missing service surfaces as the sibling's NotFound error.
type ServiceReader interface {
	Get(ctx context.Context, serviceID string) (ServiceFacts, error)
	ListByEnvironment(ctx context.Context, environmentID string) ([]ServiceFacts, error)
}

// ConfigReader lists a service's effective configuration (service + environment scope).
type ConfigReader interface {
	ListForService(ctx context.Context, serviceID string) ([]ConfigEntry, error)
}

// DomainReader lists a service's custom domains.
type DomainReader interface {
	ListByService(ctx context.Context, serviceID string) ([]DomainFact, error)
}

// DeploymentReader returns a service's latest deployment, if any (ok=false when never deployed).
type DeploymentReader interface {
	LatestForService(ctx context.Context, serviceID string) (fact DeploymentFact, ok bool, err error)
}

// ServerReader reports whether the workspace has a server ready to deploy onto.
type ServerReader interface {
	WorkspaceReadiness(ctx context.Context, workspaceID string) (ServerReadiness, error)
}

// BackupReader reports whether a (database) service has at least one backup. It is OPTIONAL:
// when nil (the backups module isn't wired yet) the backup check degrades to an informational
// "not available yet" rather than failing — so readiness ships before backups do.
type BackupReader interface {
	HasBackup(ctx context.Context, serviceID string) (bool, error)
}
