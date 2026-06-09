package agents

import (
	"context"
	"fmt"
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
		out = append(out, toProto(a, now))
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
		AgentID:      req.Msg.GetAgentId(),
		Credential:   req.Msg.GetCredential(),
		AgentVersion: req.Msg.GetAgentVersion(),
	})
	if err != nil {
		return nil, problem.ToConnect(err)
	}
	return connect.NewResponse(&agentv1.HeartbeatResponse{NextIntervalSeconds: int64(res.NextInterval.Seconds())}), nil
}

func toProto(a Agent, now time.Time) *controlplanev1.Agent {
	lastSeen := ""
	if a.LastSeenAt != nil {
		lastSeen = a.LastSeenAt.UTC().Format(time.RFC3339)
	}
	return &controlplanev1.Agent{
		Id:           a.ID,
		ServerId:     a.ServerID,
		WorkspaceId:  a.WorkspaceID,
		AgentVersion: a.AgentVersion,
		Status:       a.Status(now),
		LastSeenAt:   lastSeen,
		CreatedAt:    a.CreatedAt.UTC().Format(time.RFC3339),
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
		return fmt.Sprintf("go run ./cmd/agent --control-plane %s --token %s", publicURL, token)
	}
	return fmt.Sprintf("curl -fsSL %s | sh -s -- --control-plane %s --token %s", agentInstallScript, publicURL, token)
}
