package secrets

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

// UpsertSecret inserts or updates by (environment_id, key). The unique conflict is the
// success path (secrets are mutable), so there is no AlreadyExists mapping here. Only
// ciphertext is written, and RETURNING yields metadata only — the value is write-only.
func (s *postgresStore) UpsertSecret(ctx context.Context, tx database.Tx, environmentID, key string, ciphertext []byte) (Secret, error) {
	row, err := db.New(tx).UpsertSecret(ctx, db.UpsertSecretParams{
		EnvironmentID: environmentID,
		Key:           key,
		Ciphertext:    ciphertext,
	})
	if err != nil {
		return Secret{}, err
	}
	return Secret{
		ID:            row.ID,
		EnvironmentID: row.EnvironmentID,
		Key:           row.Key,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

func (s *postgresStore) ListByEnvironment(ctx context.Context, environmentID string) ([]Secret, error) {
	rows, err := db.New(s.pool).ListSecretsByEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	out := make([]Secret, 0, len(rows))
	for _, r := range rows {
		out = append(out, Secret{
			ID:            r.ID,
			EnvironmentID: r.EnvironmentID,
			Key:           r.Key,
			CreatedAt:     r.CreatedAt,
			UpdatedAt:     r.UpdatedAt,
		})
	}
	return out, nil
}

func (s *postgresStore) DeleteSecret(ctx context.Context, tx database.Tx, environmentID, key string) (string, bool, error) {
	deletedID, err := db.New(tx).DeleteSecret(ctx, db.DeleteSecretParams{EnvironmentID: environmentID, Key: key})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return deletedID, true, nil
}

// WorkspaceIDForEnvironment reuses the shared environment->workspace resolution query
// (an environment's owning workspace is the same lookup for any environment-scoped
// module), so this module authorizes and audits against the workspace.
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
