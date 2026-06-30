package sources

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in the module
// allowed to import internal/platform/database/db — depguard enforces it.
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) ListConnectionsByWorkspace(ctx context.Context, workspaceID string) ([]Connection, error) {
	rows, err := db.New(s.pool).ListConnectionsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(rows))
	for _, r := range rows {
		out = append(out, Connection{
			ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, Kind: r.Kind,
			AccountLogin: r.AccountLogin, AccountID: r.AccountID, InstallationID: r.InstallationID,
			Scopes: r.Scopes, ConnectedBy: r.ConnectedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

func (s *postgresStore) GetConnectionByID(ctx context.Context, connectionID string) (Connection, bool, error) {
	r, err := db.New(s.pool).GetConnectionByID(ctx, connectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Connection{}, false, nil
		}
		return Connection{}, false, err
	}
	return Connection{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, Kind: r.Kind,
		AccountLogin: r.AccountLogin, AccountID: r.AccountID, InstallationID: r.InstallationID,
		Scopes: r.Scopes, ConnectedBy: r.ConnectedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, true, nil
}

func (s *postgresStore) GetSealedTokenByConnection(ctx context.Context, connectionID string) ([]byte, bool, error) {
	ct, err := db.New(s.pool).GetSealedTokenByConnection(ctx, connectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(ct) == 0 {
		return nil, false, nil
	}
	return ct, true, nil
}

func (s *postgresStore) InsertOAuthConnection(ctx context.Context, tx database.Tx, c OAuthConnectionWrite) (Connection, error) {
	r, err := db.New(tx).InsertOAuthConnection(ctx, db.InsertOAuthConnectionParams{
		WorkspaceID:           c.WorkspaceID,
		Provider:              c.Provider,
		AccountLogin:          c.AccountLogin,
		AccountID:             c.AccountID,
		AccessTokenCiphertext: c.TokenCipher,
		Scopes:                c.Scopes,
		ConnectedBy:           c.ConnectedBy,
	})
	if err != nil {
		return Connection{}, err
	}
	return Connection{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, Kind: r.Kind,
		AccountLogin: r.AccountLogin, AccountID: r.AccountID, InstallationID: r.InstallationID,
		Scopes: r.Scopes, ConnectedBy: r.ConnectedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

func (s *postgresStore) InsertAppConnection(ctx context.Context, tx database.Tx, c AppConnectionWrite) (Connection, error) {
	installationID := c.InstallationID
	r, err := db.New(tx).InsertAppConnection(ctx, db.InsertAppConnectionParams{
		WorkspaceID:    c.WorkspaceID,
		Provider:       c.Provider,
		AccountLogin:   c.AccountLogin,
		AccountID:      c.AccountID,
		InstallationID: &installationID,
		ConnectedBy:    c.ConnectedBy,
	})
	if err != nil {
		return Connection{}, err
	}
	return Connection{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, Kind: r.Kind,
		AccountLogin: r.AccountLogin, AccountID: r.AccountID, InstallationID: r.InstallationID,
		Scopes: r.Scopes, ConnectedBy: r.ConnectedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

func (s *postgresStore) DeleteConnectionByID(ctx context.Context, tx database.Tx, connectionID string) (string, bool, error) {
	id, err := db.New(tx).DeleteConnectionByID(ctx, connectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

func (s *postgresStore) CountServicesByConnection(ctx context.Context, connectionID string) (int64, error) {
	// connection_id is nullable (public services have none); callers always pass a real id.
	return db.New(s.pool).CountServicesByConnection(ctx, &connectionID)
}
