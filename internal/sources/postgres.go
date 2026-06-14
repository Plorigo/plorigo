package sources

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in
// the module allowed to import internal/platform/database/db — depguard enforces it.
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) UpsertConnection(ctx context.Context, tx database.Tx, c ConnectionWrite) (Connection, error) {
	row, err := db.New(tx).UpsertSourceConnection(ctx, db.UpsertSourceConnectionParams{
		WorkspaceID:           c.WorkspaceID,
		Provider:              c.Provider,
		GithubLogin:           c.GitHubLogin,
		GithubUserID:          c.GitHubUserID,
		AccessTokenCiphertext: c.TokenCiphertext,
		Scopes:                c.Scopes,
		ConnectedBy:           c.ConnectedBy,
	})
	if err != nil {
		return Connection{}, err
	}
	return Connection{
		ID:           row.ID,
		WorkspaceID:  row.WorkspaceID,
		Provider:     row.Provider,
		GitHubLogin:  row.GithubLogin,
		GitHubUserID: row.GithubUserID,
		Scopes:       row.Scopes,
		ConnectedBy:  row.ConnectedBy,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

func (s *postgresStore) GetConnection(ctx context.Context, workspaceID, provider string) (Connection, bool, error) {
	row, err := db.New(s.pool).GetSourceConnectionByWorkspace(ctx, db.GetSourceConnectionByWorkspaceParams{
		WorkspaceID: workspaceID,
		Provider:    provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, false, nil
		}
		return Connection{}, false, err
	}
	return Connection{
		ID:           row.ID,
		WorkspaceID:  row.WorkspaceID,
		Provider:     row.Provider,
		GitHubLogin:  row.GithubLogin,
		GitHubUserID: row.GithubUserID,
		Scopes:       row.Scopes,
		ConnectedBy:  row.ConnectedBy,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, true, nil
}

func (s *postgresStore) GetConnectionToken(ctx context.Context, workspaceID, provider string) ([]byte, bool, error) {
	ct, err := db.New(s.pool).GetConnectionTokenByWorkspace(ctx, db.GetConnectionTokenByWorkspaceParams{
		WorkspaceID: workspaceID,
		Provider:    provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return ct, true, nil
}

func (s *postgresStore) DeleteConnection(ctx context.Context, tx database.Tx, workspaceID, provider string) (string, bool, error) {
	id, err := db.New(tx).DeleteSourceConnection(ctx, db.DeleteSourceConnectionParams{WorkspaceID: workspaceID, Provider: provider})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

func (s *postgresStore) CountServicesByConnection(ctx context.Context, connectionID string) (int64, error) {
	// connection_id is nullable (public services have none), so the generated query takes
	// *string; callers always pass a real connection id.
	return db.New(s.pool).CountServicesByConnection(ctx, &connectionID)
}
