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

// UpsertEnvVar inserts or updates by (service_id, key). The unique conflict is the
// success path, so there is no AlreadyExists mapping here (unlike the create-only
// modules).
func (s *postgresStore) UpsertEnvVar(ctx context.Context, tx database.Tx, e EnvVar) (EnvVar, error) {
	row, err := db.New(tx).UpsertEnvVar(ctx, db.UpsertEnvVarParams{
		ServiceID: e.ServiceID,
		Key:       e.Key,
		Value:     e.Value,
	})
	if err != nil {
		return EnvVar{}, err
	}
	return envVarFromRow(row), nil
}

func (s *postgresStore) ListByService(ctx context.Context, serviceID string) ([]EnvVar, error) {
	rows, err := db.New(s.pool).ListEnvVarsByService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]EnvVar, 0, len(rows))
	for _, r := range rows {
		out = append(out, envVarFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) DeleteEnvVar(ctx context.Context, tx database.Tx, serviceID, key string) (string, bool, error) {
	deletedID, err := db.New(tx).DeleteEnvVar(ctx, db.DeleteEnvVarParams{ServiceID: serviceID, Key: key})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return deletedID, true, nil
}

func (s *postgresStore) WorkspaceIDForService(ctx context.Context, serviceID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetServiceWorkspaceID(ctx, serviceID)
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
		ID:        r.ID,
		ServiceID: r.ServiceID,
		Key:       r.Key,
		Value:     r.Value,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
