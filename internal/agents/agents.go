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
	"fmt"
	"strconv"
	"strings"
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
// server can safely run a deployment. The dashboard layers the managed-setup-run states
// ("setting up" / "setup failed") on top of these from the setup flow (PLO-93/94).
const (
	ReadinessReady    = "ready"    // online; Docker (and Caddy, when reported) healthy
	ReadinessDegraded = "degraded" // online and deployable, but with a warning to act on
	ReadinessBlocked  = "blocked"  // online but a hard prerequisite is missing or unsafe
	ReadinessUnknown  = "unknown"  // offline or never connected — readiness can't be assessed
)

// Coarse resource thresholds for the readiness signal — readiness is a traffic-light, not a
// monitoring system.
const (
	diskCriticalFreeBytes = 1 << 30       // 1 GiB — below this a deploy can't pull/write an image
	diskLowFreeBytes      = 5 * (1 << 30) // 5 GiB — deployable, but warn
	memLowAvailableBytes  = 256 << 20     // 256 MiB available — builds/containers risk OOM kills

	// minSupportedDockerMajor: Docker Engine below this predates BuildKit defaults the build
	// path relies on — deployable, but warn.
	minSupportedDockerMajor = 20
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
	// Extended host-readiness facts (PLO-95). CaddyAvailable is a tri-state (nil = not
	// reported); CPUCount == 0 marks an agent that does not report the extended facts, so
	// Readiness skips the Caddy/disk/memory checks for it (backward compatibility).
	CaddyAvailable    *bool
	CaddyRunning      bool
	CaddyVersion      string
	DiskTotalBytes    int64
	DiskFreeBytes     int64
	MemTotalBytes     int64
	MemAvailableBytes int64
	CPUCount          int32
	LastSeenAt        *time.Time
	CreatedAt         time.Time
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
// reported compatibility facts; like Status it is derived, never stored. Offline/awaiting are
// single-sourced through Status, so liveness and readiness can never disagree.
//
// It returns "ready" (all good), "degraded" (deployable, with a warning), "blocked" (a hard
// prerequisite is missing or unsafe), or "unknown" (offline/never connected). The extended
// Caddy/disk/memory checks run only for agents that report them (CPUCount > 0), so older
// agents are never falsely blocked.
func (a Agent) Readiness(now time.Time) (state, reason string) {
	switch a.Status(now) {
	case StatusAwaiting:
		return ReadinessUnknown, "Waiting for the agent to connect. Run the install command on the server."
	case StatusOffline:
		return ReadinessUnknown, "Agent offline — no heartbeat in over 90 seconds. Check the machine is on and the plorigo-agent service is running."
	}

	// Hard blockers first — a deployment cannot succeed or serve traffic in these states.
	if a.OS != "" && a.OS != "linux" {
		return ReadinessBlocked, fmt.Sprintf("Unsupported host OS %q — Plorigo deploys to Linux servers.", a.OS)
	}
	if a.DockerAvailable != nil && !*a.DockerAvailable {
		return ReadinessBlocked, "Docker isn't reachable on this server. Install or start Docker; the agent recovers automatically once it's running."
	}
	if a.CPUCount > 0 {
		switch {
		case a.CaddyAvailable != nil && !*a.CaddyAvailable:
			return ReadinessBlocked, "Caddy isn't installed, so the server can't route traffic to your apps. Re-run setup to install it."
		case a.CaddyAvailable != nil && *a.CaddyAvailable && !a.CaddyRunning:
			return ReadinessBlocked, "Caddy isn't running — it may be stopped or unable to bind ports 80/443 (often another process is using them). The agent recovers it automatically; free the ports and it returns."
		case a.DiskTotalBytes > 0 && a.DiskFreeBytes < diskCriticalFreeBytes:
			return ReadinessBlocked, fmt.Sprintf("Almost no disk space left (%s free). Free space before deploying.", humanBytes(a.DiskFreeBytes))
		}
	}

	// Soft warnings — deployable, but worth surfacing so a deploy doesn't fail by surprise.
	if a.DockerAvailable == nil {
		return ReadinessDegraded, "Compatibility checks pending — update the agent to the latest version so it reports Docker and host readiness."
	}
	if m := dockerMajor(a.DockerVersion); m > 0 && m < minSupportedDockerMajor {
		return ReadinessDegraded, fmt.Sprintf("Docker %s is old; update it for reliable BuildKit builds.", a.DockerVersion)
	}
	if a.CPUCount > 0 {
		switch {
		case a.DiskTotalBytes > 0 && a.DiskFreeBytes < diskLowFreeBytes:
			return ReadinessDegraded, fmt.Sprintf("Low disk space (%s free). Deployments may fail once it runs out.", humanBytes(a.DiskFreeBytes))
		case a.MemTotalBytes > 0 && a.MemAvailableBytes < memLowAvailableBytes:
			return ReadinessDegraded, fmt.Sprintf("Low free memory (%s available). Builds or containers may be killed under load.", humanBytes(a.MemAvailableBytes))
		}
	}
	return ReadinessReady, ""
}

// dockerMajor parses the leading major version from a "24.0.7"-style string (0 if unparseable).
func dockerMajor(v string) int {
	major, _, _ := strings.Cut(strings.TrimSpace(v), ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

// humanBytes renders a coarse, human-friendly size for readiness reasons.
func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%d MiB", b/(1<<20))
	default:
		return fmt.Sprintf("%d bytes", b)
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
	// Extended host-readiness facts (PLO-95); see Agent for the tri-state / sentinel rules.
	CaddyAvailable    *bool
	CaddyRunning      bool
	CaddyVersion      string
	DiskTotalBytes    int64
	DiskFreeBytes     int64
	MemTotalBytes     int64
	MemAvailableBytes int64
	CPUCount          int32
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
