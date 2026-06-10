package agents

import (
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"time"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// service is the business logic. It orchestrates ports only — no SQL, no transport.
// Dashboard-facing mutations authorize the caller (workspace resolved from the server)
// before the WithinTx block and audit inside it (modules.md, Rule 4). The agent-facing
// RPCs authenticate by the one-time token / credential carried in the request, not a
// user session, so they do not go through the authorizer.
type service struct {
	tx         TxRunner
	store      Store
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, authorizer: authorizer, audit: audit, log: log}
}

var _ Service = (*service)(nil)

// CreateRegistrationToken mints a one-time token a dashboard user installs onto a
// server. It resolves the server's workspace, authorizes the caller against it, and
// audits the mint in the same transaction.
func (s *service) CreateRegistrationToken(ctx context.Context, serverID string) (RegistrationToken, error) {
	if _, err := id.Parse(serverID); err != nil {
		return RegistrationToken{}, problem.InvalidInput("a valid server_id is required")
	}
	workspaceID, ok, err := s.store.WorkspaceIDForServer(ctx, serverID)
	if err != nil {
		return RegistrationToken{}, problem.Internalf(err, "create registration token")
	}
	if !ok {
		return RegistrationToken{}, problem.NotFound("server %s not found", serverID)
	}

	caller := principal.FromContext(ctx)
	if err := s.authorizer.Authorize(ctx, caller, authz.ActionAgentCreate, authz.Resource{Type: "agent", WorkspaceID: workspaceID, ID: serverID}); err != nil {
		return RegistrationToken{}, err
	}

	raw, hash, err := newRegistrationToken()
	if err != nil {
		return RegistrationToken{}, problem.Internalf(err, "create registration token")
	}
	expiresAt := time.Now().Add(registrationTokenTTL)
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		if err := s.store.InsertRegistrationToken(ctx, tx, RegistrationTokenRow{
			ServerID:    serverID,
			WorkspaceID: workspaceID,
			TokenHash:   hash,
			CreatedBy:   caller.UserID,
			ExpiresAt:   expiresAt,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, tx, "agent.registration_token.create", "server", serverID, workspaceID, caller.UserID)
	})
	if err != nil {
		return RegistrationToken{}, problem.Internalf(err, "create registration token")
	}
	s.log.Info("agent registration token created", "server_id", serverID, "workspace_id", workspaceID, "actor", caller.UserID)
	return RegistrationToken{Raw: raw, ServerID: serverID, ExpiresAt: expiresAt}, nil
}

// ListByWorkspace returns the agents in a workspace (workspace-scoped, authorized).
func (s *service) ListByWorkspace(ctx context.Context, workspaceID string) ([]Agent, error) {
	if workspaceID == "" {
		return nil, problem.InvalidInput("workspace_id is required")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionAgentRead, authz.Resource{Type: "agent", WorkspaceID: workspaceID}); err != nil {
		return nil, err
	}
	return s.store.ListByWorkspace(ctx, workspaceID)
}

// Register redeems a one-time token (consuming it), records the agent's public key, and
// issues a durable credential. Token consumption and the agent upsert commit together,
// so a failed registration leaves the token unspent.
func (s *service) Register(ctx context.Context, in RegisterInput) (Registered, error) {
	if in.RegistrationToken == "" {
		return Registered{}, problem.InvalidInput("a registration token is required")
	}
	if len(in.PublicKey) != ed25519.PublicKeySize {
		return Registered{}, problem.InvalidInput("a valid ed25519 public key is required")
	}

	credential, credHash, err := newAgentCredential()
	if err != nil {
		return Registered{}, problem.Internalf(err, "register agent")
	}

	var agent Agent
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		consumed, ok, err := s.store.ConsumeRegistrationToken(ctx, tx, hashToken(in.RegistrationToken))
		if err != nil {
			return err
		}
		if !ok {
			return problem.PermissionDenied("registration token is invalid or expired")
		}
		agent, err = s.store.UpsertAgent(ctx, tx, AgentUpsert{
			ServerID:       consumed.ServerID,
			WorkspaceID:    consumed.WorkspaceID,
			PublicKey:      in.PublicKey,
			CredentialHash: credHash,
			AgentVersion:   in.AgentVersion,
		})
		if err != nil {
			return err
		}
		// The agent is not a user; record it as a non-user actor for the audit trail.
		return s.audit.Record(ctx, tx, "agent.register", "agent", agent.ID, agent.WorkspaceID, "agent:"+agent.ID)
	})
	if err != nil {
		return Registered{}, mapErr(err, "register agent")
	}
	s.log.Info("agent registered", "agent_id", agent.ID, "server_id", agent.ServerID, "workspace_id", agent.WorkspaceID, "version", in.AgentVersion)
	return Registered{AgentID: agent.ID, Credential: credential}, nil
}

// Heartbeat validates the durable credential and records liveness. It is high-frequency
// and therefore not audited (only the authorized mint and the registration are).
func (s *service) Heartbeat(ctx context.Context, in HeartbeatInput) (HeartbeatResult, error) {
	if in.Credential == "" {
		return HeartbeatResult{}, problem.InvalidInput("a credential is required")
	}
	agent, ok, err := s.store.Heartbeat(ctx, hashToken(in.Credential), HeartbeatFacts{
		AgentVersion:    in.AgentVersion,
		DockerAvailable: in.DockerAvailable,
		DockerVersion:   in.DockerVersion,
		OS:              in.OS,
		Arch:            in.Arch,
	})
	if err != nil {
		return HeartbeatResult{}, problem.Internalf(err, "heartbeat")
	}
	if !ok {
		return HeartbeatResult{}, problem.PermissionDenied("unknown agent credential")
	}
	s.log.Debug("agent heartbeat", "agent_id", agent.ID, "server_id", agent.ServerID, "version", in.AgentVersion)
	return HeartbeatResult{NextInterval: heartbeatInterval}, nil
}

// mapErr preserves domain (*problem.Error) errors and wraps anything else as internal.
func mapErr(err error, op string) error {
	if err == nil {
		return nil
	}
	var pe *problem.Error
	if errors.As(err, &pe) {
		return err
	}
	return problem.Internalf(err, "%s", op)
}
