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

// Server readiness states, derived (never stored) from liveness plus the agent's reported
// compatibility facts. This is what the dashboard leads with on a server card: whether the
// server can safely run a deployment.
const (
	ReadinessReady       = "ready"       // online and Docker is available
	ReadinessDegraded    = "degraded"    // online but Docker is unavailable, or facts unknown
	ReadinessUnavailable = "unavailable" // offline or never connected
)

// Agent is the program registered to a server. The control plane stores its public key,
// derives liveness from the last heartbeat, and records the compatibility facts the agent
// reports each beat.
type Agent struct {
	ID           string
	ServerID     string
	WorkspaceID  string
	AgentVersion string
	// Reported compatibility facts (see HeartbeatInput). DockerAvailable is a tri-state:
	// nil means the agent hasn't reported health yet (or predates the feature).
	DockerAvailable *bool
	DockerVersion   string
	OS              string
	Arch            string
	LastSeenAt      *time.Time
	CreatedAt       time.Time
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

// Readiness derives whether the server can safely run a deployment at time now, plus a
// plain-English reason a user can act on without SSHing in. It composes liveness with the
// reported Docker facts; like Status it is derived, never stored. Offline/awaiting are
// single-sourced through Status, so liveness and readiness can never disagree.
func (a Agent) Readiness(now time.Time) (state, reason string) {
	switch a.Status(now) {
	case StatusAwaiting:
		return ReadinessUnavailable, "Waiting for the agent to connect. Run the install command on the server."
	case StatusOffline:
		return ReadinessUnavailable, "Agent offline — no heartbeat in over 90 seconds. Check the machine is on and the plorigo-agent service is running."
	}
	switch {
	case a.DockerAvailable == nil:
		return ReadinessDegraded, "Compatibility checks pending — update the agent to the latest version so it reports Docker and host readiness."
	case !*a.DockerAvailable:
		return ReadinessDegraded, "Docker isn't reachable on this server. Install or start Docker; the agent recovers automatically once it's running."
	default:
		return ReadinessReady, ""
	}
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

// HeartbeatInput is what an agent presents on each heartbeat: its identity plus the
// compatibility facts it observed. DockerAvailable is nil when the agent did not report
// health (it predates the feature), which the control plane renders as "checks pending"
// rather than as "Docker down".
type HeartbeatInput struct {
	AgentID         string
	Credential      string
	AgentVersion    string
	DockerAvailable *bool
	DockerVersion   string
	OS              string
	Arch            string
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
