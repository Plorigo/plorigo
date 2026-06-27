package agents

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"connectrpc.com/connect"

	"github.com/plorigo/plorigo/internal/platform/problem"
	agentv1 "github.com/plorigo/plorigo/proto/gen/agent/v1"
	"github.com/plorigo/plorigo/proto/gen/agent/v1/agentv1connect"
	controlplanev1 "github.com/plorigo/plorigo/proto/gen/controlplane/v1"
	"github.com/plorigo/plorigo/proto/gen/controlplane/v1/controlplanev1connect"
)

// adminHandler adapts the dashboard-facing controlplane.v1.AgentService to the domain
// Service. It maps proto <-> domain and domain errors -> connect codes; no business
// logic here.
type adminHandler struct {
	svc       Service
	publicURL string
	dev       bool
	now       func() time.Time
}

var _ controlplanev1connect.AgentServiceHandler = (*adminHandler)(nil)

func (h *adminHandler) CreateRegistrationToken(ctx context.Context, req *connect.Request[controlplanev1.CreateRegistrationTokenRequest]) (*connect.Response[controlplanev1.CreateRegistrationTokenResponse], error) {
	tok, err := h.svc.CreateRegistrationToken(ctx, req.Msg.GetServerId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&controlplanev1.CreateRegistrationTokenResponse{
		RegistrationToken: tok.Raw,
		InstallCommand:    installCommand(h.publicURL, tok.Raw, h.dev),
		ExpiresAt:         tok.ExpiresAt.UTC().Format(time.RFC3339),
	}), nil
}

func (h *adminHandler) ListAgentsByWorkspace(ctx context.Context, req *connect.Request[controlplanev1.ListAgentsByWorkspaceRequest]) (*connect.Response[controlplanev1.ListAgentsByWorkspaceResponse], error) {
	agents, err := h.svc.ListByWorkspace(ctx, req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	now := h.now()
	out := make([]*controlplanev1.Agent, 0, len(agents))
	for _, a := range agents {
		out = append(out, toProto(a, now, h.dev))
	}
	return connect.NewResponse(&controlplanev1.ListAgentsByWorkspaceResponse{Agents: out}), nil
}

// gatewayHandler adapts the agent-facing agent.v1.AgentService to the domain Service.
// Its procedures are public at the auth interceptor; the service validates the
// registration token / credential carried in the request body.
type gatewayHandler struct {
	svc Service
}

var _ agentv1connect.AgentServiceHandler = (*gatewayHandler)(nil)

func (h *gatewayHandler) Register(ctx context.Context, req *connect.Request[agentv1.RegisterRequest]) (*connect.Response[agentv1.RegisterResponse], error) {
	reg, err := h.svc.Register(ctx, RegisterInput{
		RegistrationToken: req.Msg.GetRegistrationToken(),
		PublicKey:         req.Msg.GetPublicKey(),
		AgentVersion:      req.Msg.GetAgentVersion(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.RegisterResponse{AgentId: reg.AgentID, Credential: reg.Credential}), nil
}

func (h *gatewayHandler) Heartbeat(ctx context.Context, req *connect.Request[agentv1.HeartbeatRequest]) (*connect.Response[agentv1.HeartbeatResponse], error) {
	res, err := h.svc.Heartbeat(ctx, HeartbeatInput{
		AgentID:           req.Msg.GetAgentId(),
		Credential:        req.Msg.GetCredential(),
		AgentVersion:      req.Msg.GetAgentVersion(),
		DockerAvailable:   reportedDockerAvailable(req.Msg),
		DockerVersion:     clampFact(req.Msg.GetDockerVersion()),
		OS:                clampFact(req.Msg.GetOs()),
		Arch:              clampFact(req.Msg.GetArch()),
		CaddyAvailable:    reportedCaddyAvailable(req.Msg),
		CaddyRunning:      req.Msg.GetCaddyRunning(),
		CaddyVersion:      clampFact(req.Msg.GetCaddyVersion()),
		DiskTotalBytes:    clampBytes(req.Msg.GetDiskTotalBytes()),
		DiskFreeBytes:     clampBytes(req.Msg.GetDiskFreeBytes()),
		MemTotalBytes:     clampBytes(req.Msg.GetMemTotalBytes()),
		MemAvailableBytes: clampBytes(req.Msg.GetMemAvailableBytes()),
		CPUCount:          clampCPUCount(req.Msg.GetCpuCount()),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.HeartbeatResponse{NextIntervalSeconds: int64(res.NextInterval.Seconds())}), nil
}

// maxFactLen caps an agent-reported compatibility fact before it is stored or shown. The
// agent gateway is authenticated only by the credential carried in the request, so these
// strings are untrusted; bounding them keeps a misbehaving agent from writing an unbounded
// value into a text column. Real values (a semver, a GOOS, a GOARCH) are far shorter.
const maxFactLen = 64

func clampFact(s string) string {
	if len(s) <= maxFactLen {
		return s
	}
	r := []rune(s)
	if len(r) > maxFactLen {
		r = r[:maxFactLen]
	}
	return string(r)
}

// reportedDockerAvailable distinguishes "the agent reported Docker is down" from "the
// agent did not report health at all". A health-reporting agent always sets os
// (runtime.GOOS, never empty), so an empty os means the facts are absent and Docker
// availability is unknown (nil) — rendered as "checks pending", not as a false alarm.
func reportedDockerAvailable(m *agentv1.HeartbeatRequest) *bool {
	if m.GetOs() == "" {
		return nil
	}
	v := m.GetDockerAvailable()
	return &v
}

// reportedCaddyAvailable applies the same tri-state logic to the extended (PLO-95) facts.
// An agent that reports them always sets cpu_count (runtime.NumCPU, never zero), so a zero
// cpu_count means the extended facts are absent — Caddy availability is unknown (nil), and
// readiness skips the Caddy/disk/memory checks rather than falsely blocking an older agent.
func reportedCaddyAvailable(m *agentv1.HeartbeatRequest) *bool {
	if m.GetCpuCount() == 0 {
		return nil
	}
	v := m.GetCaddyAvailable()
	return &v
}

// clampCPUCount bounds an untrusted, agent-reported CPU count before it is stored.
func clampCPUCount(n uint32) int32 {
	const maxCPU = 4096
	if n > maxCPU {
		return maxCPU
	}
	return int32(n)
}

// clampBytes drops a nonsensical negative byte count from an untrusted agent to zero
// ("not reported"), so it can never poison the readiness thresholds.
func clampBytes(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
}

func toProto(a Agent, now time.Time, allowNonLinuxHost bool) *controlplanev1.Agent {
	lastSeen := ""
	if a.LastSeenAt != nil {
		lastSeen = a.LastSeenAt.UTC().Format(time.RFC3339)
	}
	readiness, reason := a.Readiness(now, allowNonLinuxHost)
	return &controlplanev1.Agent{
		Id:                a.ID,
		ServerId:          a.ServerID,
		WorkspaceId:       a.WorkspaceID,
		AgentVersion:      a.AgentVersion,
		Status:            a.Status(now),
		Readiness:         readiness,
		ReadinessReason:   reason,
		DockerAvailable:   a.DockerAvailable != nil && *a.DockerAvailable,
		DockerVersion:     a.DockerVersion,
		Os:                a.OS,
		Arch:              a.Arch,
		CaddyAvailable:    a.CaddyAvailable != nil && *a.CaddyAvailable,
		CaddyRunning:      a.CaddyRunning,
		CaddyVersion:      a.CaddyVersion,
		DiskTotalBytes:    a.DiskTotalBytes,
		DiskFreeBytes:     a.DiskFreeBytes,
		MemTotalBytes:     a.MemTotalBytes,
		MemAvailableBytes: a.MemAvailableBytes,
		CpuCount:          uint32(a.CPUCount),
		LastSeenAt:        lastSeen,
		CreatedAt:         a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// agentInstallScript is the public installer the one-line command fetches; it installs
// the agent binary and a systemd service (see scripts/install-agent.sh).
const agentInstallScript = "https://raw.githubusercontent.com/Plorigo/plorigo/main/scripts/install-agent.sh"

// installCommand renders the agent install command shown in the dashboard. The token is
// single-use and short-lived; publicURL is the control plane's public URL — the RPC
// endpoint the agent connects to, NOT the dashboard origin. The command follows the
// environment the control plane runs in: in production it fetches the public installer
// script; in dev it runs the agent from the local source checkout, so a developer tests
// their working copy instead of installing the published agent.
func installCommand(publicURL, token string, dev bool) string {
	if dev {
		caddyHTTP, caddyAdmin := devCaddyPorts(publicURL)
		return fmt.Sprintf("go run ./cmd/agent --control-plane %s --token %s --caddy-config .context/plorigo-agent.Caddyfile --caddy-http-port %d --caddy-admin 127.0.0.1:%d", publicURL, token, caddyHTTP, caddyAdmin)
	}
	// `sudo sh`: the installer prepares the host (installs Docker/Caddy, writes the systemd
	// unit) and so requires root; on a fresh VPS root login sudo is a harmless no-op.
	return fmt.Sprintf("curl -fsSL %s | sudo sh -s -- --control-plane %s --token %s", agentInstallScript, publicURL, token)
}

func devCaddyPorts(publicURL string) (httpPort, adminPort int) {
	const (
		defaultHTTP  = 8083
		defaultAdmin = 8084
	)
	u, err := url.Parse(publicURL)
	if err != nil {
		return defaultHTTP, defaultAdmin
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65531 {
		return defaultHTTP, defaultAdmin
	}
	return port + 3, port + 4
}
