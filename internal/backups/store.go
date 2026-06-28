package backups

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port the backups service needs. Implemented by postgres.go, faked in
// tests. It also performs a few sibling-table reads (services, deployments, agents, config) for
// target/credential resolution, which modules.md Rule 2 permits from a module's postgres.go.
type Store interface {
	InsertBackup(ctx context.Context, tx database.Tx, b NewBackup) (Backup, error)
	GetBackup(ctx context.Context, id string) (Backup, bool, error)
	ListByService(ctx context.Context, serviceID string) ([]Backup, error)
	// ClaimNextForServer atomically claims the oldest queued backup for a server (status ->
	// assigned). ok is false (nil error) when there is no queued work.
	ClaimNextForServer(ctx context.Context, tx database.Tx, serverID string) (Backup, bool, error)
	// UpdateStatus records a status transition; a zero/empty value never clobbers a set one.
	UpdateStatus(ctx context.Context, tx database.Tx, u StatusUpdate) (Backup, error)

	// ServiceTarget resolves the database service a backup targets (its denormalized
	// workspace/project/environment and its source/template kind). ok is false when missing.
	ServiceTarget(ctx context.Context, serviceID string) (ServiceTarget, bool, error)
	// RunningServerForService returns the server the service's current running container is on —
	// the backup must run on that server's agent. ok is false when it is not running.
	RunningServerForService(ctx context.Context, serviceID string) (serverID string, ok bool, err error)
	// AgentServerByCredential resolves the agent and its server from a durable agent credential
	// hash, scoping agent-facing work to its own server. ok is false when no agent matches.
	AgentServerByCredential(ctx context.Context, credentialHash []byte) (agentID, serverID string, ok bool, err error)
	// DBCredentialsForService resolves the managed database's POSTGRES_* connection credentials
	// from the service's configuration (plaintext variables written at provision time).
	DBCredentialsForService(ctx context.Context, serviceID string) (DBCredentials, error)

	// Restore jobs.
	InsertRestore(ctx context.Context, tx database.Tx, r NewRestore) (RestoreJob, error)
	GetRestore(ctx context.Context, id string) (RestoreJob, bool, error)
	ListRestoresByService(ctx context.Context, serviceID string) ([]RestoreJob, error)
	ClaimNextRestoreForServer(ctx context.Context, tx database.Tx, serverID string) (RestoreJob, bool, error)
	UpdateRestoreStatus(ctx context.Context, tx database.Tx, u RestoreStatusUpdate) (RestoreJob, error)
}

// NewRestore is the data to insert a queued restore. artifact_uri is copied from the source
// backup so the agent's claim is self-contained.
type NewRestore struct {
	BackupID      string
	ServiceID     string
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	ServerID      string
	ArtifactURI   string
}

// RestoreStatusUpdate is an agent's reported transition for a restore.
type RestoreStatusUpdate struct {
	RestoreID string
	Status    string
	Message   string
	Error     string
}

// ServiceTarget is the database service a backup targets.
type ServiceTarget struct {
	ID            string
	Name          string
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	SourceKind    string // "template" for a managed database
	TemplateID    string // "postgres"
}

// DBCredentials are the managed Postgres connection credentials read from the service's config.
type DBCredentials struct {
	User     string
	Password string
	Database string
}

// NewBackup is the data to insert a queued backup.
type NewBackup struct {
	ServiceID     string
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	ServerID      string
	Label         string // optional operator-typed name
	TriggerSource string // "manual" or "scheduled"
}

// StatusUpdate is an agent's reported transition for a backup. A zero/empty artifact_uri /
// size / checksum / message / error never clobbers a value already set.
type StatusUpdate struct {
	BackupID    string
	Status      string
	ArtifactURI string
	SizeBytes   int64
	Checksum    string
	Message     string
	Error       string
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED audit port. *audit.Service satisfies it structurally —
// backups never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
