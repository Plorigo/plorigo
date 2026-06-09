// Package agents is the control-plane side of the server agent: registration and
// liveness. It mints one-time registration tokens (authorized, workspace-scoped),
// validates an agent's token at Register — exchanging it for a durable credential and
// recording the agent's ed25519 public key — and records heartbeats so the dashboard
// can show a server online or offline.
//
// It serves TWO ConnectRPC surfaces: controlplane.v1.AgentService (dashboard-facing,
// session/token-authenticated and policy-authorized) and agent.v1.AgentService (the
// agent gateway, authenticated by the registration token / agent credential carried in
// the request, NOT a user session). See docs/architecture/agent.md and modules.md.
package agents

import (
	"context"
	"time"
)

const (
	// onlineWindow is how long after its last heartbeat an agent is still considered
	// online. Heartbeats are ~onlineWindow/3 apart, so one missed beat is tolerated.
	onlineWindow = 90 * time.Second

	// registrationTokenTTL is how long a one-time registration token stays valid.
	registrationTokenTTL = time.Hour

	// heartbeatInterval is what the control plane asks agents to wait between beats.
	heartbeatInterval = 30 * time.Second
)

// Agent liveness states, derived from the last heartbeat (never stored).
const (
	StatusAwaiting = "awaiting" // registered, but no heartbeat yet
	StatusOnline   = "online"
	StatusOffline  = "offline"
)

// Agent is the program registered to a server. The control plane stores its public
// key and derives liveness from the last heartbeat.
type Agent struct {
	ID           string
	ServerID     string
	WorkspaceID  string
	AgentVersion string
	LastSeenAt   *time.Time
	CreatedAt    time.Time
}

// Status derives the agent's liveness at time now from its last heartbeat.
func (a Agent) Status(now time.Time) string {
	if a.LastSeenAt == nil {
		return StatusAwaiting
	}
	if now.Sub(*a.LastSeenAt) <= onlineWindow {
		return StatusOnline
	}
	return StatusOffline
}

// RegistrationToken is a one-time credential authorizing a single registration onto a
// specific server. Raw is returned to the dashboard once and never stored in clear.
type RegistrationToken struct {
	Raw       string
	ServerID  string
	ExpiresAt time.Time
}

// RegisterInput is what an agent presents to register.
type RegisterInput struct {
	RegistrationToken string
	PublicKey         []byte
	AgentVersion      string
}

// Registered is the result of a successful registration.
type Registered struct {
	AgentID    string
	Credential string // durable agent credential, returned once
}

// HeartbeatInput is what an agent presents on each heartbeat.
type HeartbeatInput struct {
	AgentID      string
	Credential   string
	AgentVersion string
}

// HeartbeatResult tells the agent when to send its next heartbeat.
type HeartbeatResult struct {
	NextInterval time.Duration
}

// Service is the surface other code depends on. It backs both the dashboard-facing
// controlplane.v1.AgentService and the agent-facing agent.v1.AgentService.
type Service interface {
	CreateRegistrationToken(ctx context.Context, serverID string) (RegistrationToken, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Agent, error)
	Register(ctx context.Context, in RegisterInput) (Registered, error)
	Heartbeat(ctx context.Context, in HeartbeatInput) (HeartbeatResult, error)
}
