package agents

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// Store is the repository port. Implemented by postgres.go, faked in tests. Mutations
// that must commit with an audit row take a database.Tx.
type Store interface {
	// WorkspaceIDForServer resolves a server's owning workspace, so this server-scoped
	// module authorizes/audits against the workspace WITHOUT importing servers (Rule 4).
	// ok is false (nil error) when the server does not exist.
	WorkspaceIDForServer(ctx context.Context, serverID string) (workspaceID string, ok bool, err error)
	InsertRegistrationToken(ctx context.Context, tx database.Tx, t RegistrationTokenRow) error
	// ConsumeRegistrationToken atomically validates+consumes a one-time token by hash.
	// ok is false (nil error) when the token is unknown, already used, or expired.
	ConsumeRegistrationToken(ctx context.Context, tx database.Tx, tokenHash []byte) (c ConsumedToken, ok bool, err error)
	UpsertAgent(ctx context.Context, tx database.Tx, a AgentUpsert) (Agent, error)
	// Heartbeat records liveness and the latest reported facts, returning the agent
	// matching the credential hash. ok is false (nil error) when no agent has that credential.
	Heartbeat(ctx context.Context, credentialHash []byte, facts HeartbeatFacts) (a Agent, ok bool, err error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Agent, error)
}

// HeartbeatFacts is the mutable data an agent reports on each heartbeat: its version and
// the compatibility facts. DockerAvailable is nil when the agent did not report health.
type HeartbeatFacts struct {
	AgentVersion    string
	DockerAvailable *bool
	DockerVersion   string
	OS              string
	Arch            string
	// Extended host-readiness facts (PLO-95). CaddyAvailable is a tri-state (nil = not
	// reported); CPUCount == 0 marks an agent that does not report the extended facts.
	CaddyAvailable    *bool
	CaddyRunning      bool
	CaddyVersion      string
	DiskTotalBytes    int64
	DiskFreeBytes     int64
	MemTotalBytes     int64
	MemAvailableBytes int64
	CPUCount          int32
}

// RegistrationTokenRow is the persisted form of a minted registration token.
type RegistrationTokenRow struct {
	ServerID    string
	WorkspaceID string
	TokenHash   []byte
	CreatedBy   string
	ExpiresAt   time.Time
}

// ConsumedToken is what a successfully consumed registration token yields.
type ConsumedToken struct {
	ServerID    string
	WorkspaceID string
}

// AgentUpsert is the data to register (or re-register) an agent for a server.
type AgentUpsert struct {
	ServerID       string
	WorkspaceID    string
	PublicKey      []byte
	CredentialHash []byte
	AgentVersion   string
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared here
// as a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what agents needs from the audit module.
// *audit.Service satisfies it structurally — agents never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
