package envvars

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file
// in the module allowed to import internal/platform/database/db — depguard enforces
// it (see .golangci.yml).
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

// UpsertEnvVar inserts or updates by (environment_id, key). The unique conflict is the
// success path, so there is no AlreadyExists mapping here (unlike the create-only
// modules).
func (s *postgresStore) UpsertEnvVar(ctx context.Context, tx database.Tx, e EnvVar) (EnvVar, error) {
	row, err := db.New(tx).UpsertEnvVar(ctx, db.UpsertEnvVarParams{
		EnvironmentID: e.EnvironmentID,
		Key:           e.Key,
		Value:         e.Value,
	})
	if err != nil {
		return EnvVar{}, err
	}
	return envVarFromRow(row), nil
}

func (s *postgresStore) ListByEnvironment(ctx context.Context, environmentID string) ([]EnvVar, error) {
	rows, err := db.New(s.pool).ListEnvVarsByEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	out := make([]EnvVar, 0, len(rows))
	for _, r := range rows {
		out = append(out, envVarFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) DeleteEnvVar(ctx context.Context, tx database.Tx, environmentID, key string) (string, bool, error) {
	deletedID, err := db.New(tx).DeleteEnvVar(ctx, db.DeleteEnvVarParams{EnvironmentID: environmentID, Key: key})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return deletedID, true, nil
}

func (s *postgresStore) WorkspaceIDForEnvironment(ctx context.Context, environmentID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetEnvironmentWorkspaceID(ctx, environmentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}

// envVarFromRow maps the env_vars-table model. WorkspaceID is resolved separately for
// authorization/auditing and filled in by the service.
func envVarFromRow(r db.EnvVar) EnvVar {
	return EnvVar{
		ID:            r.ID,
		EnvironmentID: r.EnvironmentID,
		Key:           r.Key,
		Value:         r.Value,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}
