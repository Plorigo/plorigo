package app

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/agents"
	"github.com/plorigo/plorigo/internal/serversetup"
)

// agentSetupAdapter bridges the serversetup module to the agents module at the wiring layer,
// so serversetup never imports agents. It satisfies serversetup.AgentProvisioner: minting a
// one-time registration token for the installer, and reporting whether a server's agent has
// checked in (the bootstrap's heartbeat wait).
type agentSetupAdapter struct {
	agents agents.Service
	now    func() time.Time
}

var _ serversetup.AgentProvisioner = agentSetupAdapter{}

func (a agentSetupAdapter) RegistrationToken(ctx context.Context, serverID string) (string, error) {
	tok, err := a.agents.CreateRegistrationToken(ctx, serverID)
	if err != nil {
		return "", err
	}
	return tok.Raw, nil
}

func (a agentSetupAdapter) AgentOnline(ctx context.Context, workspaceID, serverID string) (bool, error) {
	list, err := a.agents.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return false, err
	}
	now := a.now()
	for _, ag := range list {
		if ag.ServerID == serverID {
			return ag.Status(now) == agents.StatusOnline, nil
		}
	}
	return false, nil
}
