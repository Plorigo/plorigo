// Package backups creates and tracks database backups for managed Postgres services. It is a
// privileged module: the agent on the database's server runs pg_dump inside the container and
// writes the dump to the server's own disk (the MVP "local" destination; an S3-compatible
// destination is a later slice). The control plane records the backup, resolves the database
// credentials per job (so the agent never reads container env), and tracks status — mirroring the
// deployment job model. See docs/architecture/backups.md and docs/architecture/security.md.
package backups

import (
	"context"
	"time"
)

// Status vocabulary, mirrored in the agent and the DB CHECK constraint (00025_backups.sql).
const (
	StatusQueued    = "queued"
	StatusAssigned  = "assigned"
	StatusDumping   = "dumping"
	StatusUploading = "uploading"
	StatusVerifying = "verifying"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// DestinationLocal is the MVP artifact destination: the server's own disk.
const DestinationLocal = "local"

// Engine + kind + template identifiers shared with the agent protocol.
const (
	EnginePostgres   = "postgres"
	KindBackup       = "backup"
	templatePostgres = "postgres"
	sourceTemplate   = "template"
)

// Backup is one attempt to capture a managed database's data.
type Backup struct {
	ID            string
	ServiceID     string
	EnvironmentID string
	ProjectID     string
	WorkspaceID   string
	ServerID      string
	Destination   string
	ArtifactURI   string
	SizeBytes     int64
	Checksum      string
	Status        string
	Message       string
	Error         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Claimed is a backup job handed to the agent, with the resolved database credentials.
type Claimed struct {
	HasWork    bool
	BackupID   string
	Kind       string // "backup"
	ServiceID  string
	Engine     string // "postgres"
	PgUser     string
	PgPassword string
	PgDatabase string
}

// PollInput is an agent's poll for the next backup job, authenticated by its credential.
type PollInput struct {
	AgentID    string
	Credential string
}

// ReportInput is an agent's reported transition for a backup it is executing.
type ReportInput struct {
	AgentID     string
	Credential  string
	BackupID    string
	Status      string
	ArtifactURI string
	SizeBytes   int64
	Checksum    string
	Message     string
	Error       string
}

// Service is the backups module surface: the dashboard-facing create/read methods plus the
// agent-facing gateway (credential-authenticated, not policy-authorized).
type Service interface {
	CreateBackup(ctx context.Context, serviceID string) (Backup, error)
	GetBackup(ctx context.Context, backupID string) (Backup, error)
	ListByService(ctx context.Context, serviceID string) ([]Backup, error)

	PollBackupJob(ctx context.Context, in PollInput) (Claimed, error)
	ReportBackupJob(ctx context.Context, in ReportInput) error
}
