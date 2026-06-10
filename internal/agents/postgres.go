package agents

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file
// in the module allowed to import internal/platform/database/db — depguard enforces it
// (see .golangci.yml).
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) WorkspaceIDForServer(ctx context.Context, serverID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetServerWorkspace(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}

func (s *postgresStore) InsertRegistrationToken(ctx context.Context, tx database.Tx, t RegistrationTokenRow) error {
	_, err := db.New(tx).CreateAgentRegistrationToken(ctx, db.CreateAgentRegistrationTokenParams{
		ServerID:    t.ServerID,
		WorkspaceID: t.WorkspaceID,
		TokenHash:   t.TokenHash,
		CreatedBy:   t.CreatedBy,
		ExpiresAt:   t.ExpiresAt,
	})
	return err
}

func (s *postgresStore) ConsumeRegistrationToken(ctx context.Context, tx database.Tx, tokenHash []byte) (ConsumedToken, bool, error) {
	row, err := db.New(tx).ConsumeAgentRegistrationToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ConsumedToken{}, false, nil
		}
		return ConsumedToken{}, false, err
	}
	return ConsumedToken{ServerID: row.ServerID, WorkspaceID: row.WorkspaceID}, true, nil
}

func (s *postgresStore) UpsertAgent(ctx context.Context, tx database.Tx, a AgentUpsert) (Agent, error) {
	row, err := db.New(tx).UpsertAgent(ctx, db.UpsertAgentParams{
		ServerID:       a.ServerID,
		WorkspaceID:    a.WorkspaceID,
		PublicKey:      a.PublicKey,
		CredentialHash: a.CredentialHash,
		AgentVersion:   a.AgentVersion,
	})
	if err != nil {
		return Agent{}, err
	}
	return Agent{
		ID:              row.ID,
		ServerID:        row.ServerID,
		WorkspaceID:     row.WorkspaceID,
		AgentVersion:    row.AgentVersion,
		DockerAvailable: row.DockerAvailable,
		DockerVersion:   row.DockerVersion,
		OS:              row.Os,
		Arch:            row.Arch,
		LastSeenAt:      row.LastSeenAt,
		CreatedAt:       row.CreatedAt,
	}, nil
}

func (s *postgresStore) Heartbeat(ctx context.Context, credentialHash []byte, facts HeartbeatFacts) (Agent, bool, error) {
	row, err := db.New(s.pool).HeartbeatAgent(ctx, db.HeartbeatAgentParams{
		CredentialHash:  credentialHash,
		AgentVersion:    facts.AgentVersion,
		DockerAvailable: facts.DockerAvailable,
		DockerVersion:   facts.DockerVersion,
		Os:              facts.OS,
		Arch:            facts.Arch,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Agent{}, false, nil
		}
		return Agent{}, false, err
	}
	return Agent{
		ID:              row.ID,
		ServerID:        row.ServerID,
		WorkspaceID:     row.WorkspaceID,
		AgentVersion:    row.AgentVersion,
		DockerAvailable: row.DockerAvailable,
		DockerVersion:   row.DockerVersion,
		OS:              row.Os,
		Arch:            row.Arch,
		LastSeenAt:      row.LastSeenAt,
		CreatedAt:       row.CreatedAt,
	}, true, nil
}

func (s *postgresStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Agent, error) {
	rows, err := db.New(s.pool).ListAgentsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(rows))
	for _, r := range rows {
		out = append(out, Agent{
			ID:              r.ID,
			ServerID:        r.ServerID,
			WorkspaceID:     r.WorkspaceID,
			AgentVersion:    r.AgentVersion,
			DockerAvailable: r.DockerAvailable,
			DockerVersion:   r.DockerVersion,
			OS:              r.Os,
			Arch:            r.Arch,
			LastSeenAt:      r.LastSeenAt,
			CreatedAt:       r.CreatedAt,
		})
	}
	return out, nil
}
