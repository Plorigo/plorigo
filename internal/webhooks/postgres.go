package webhooks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in the module
// allowed to import internal/platform/database/db — depguard enforces it. It reads source_connections
// and services (sibling tables) to resolve a delivery's installation → workspace → services, which
// modules.md Rule 2 permits from a module's postgres.go.
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) WorkspaceForInstallation(ctx context.Context, installationID string) (string, bool, error) {
	iid := installationID
	workspaceID, err := db.New(s.pool).GetWorkspaceByInstallation(ctx, &iid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}

func (s *postgresStore) ServicesForRepo(ctx context.Context, workspaceID, owner, repo string) ([]string, error) {
	return db.New(s.pool).ListServiceIDsForRepo(ctx, db.ListServiceIDsForRepoParams{
		WorkspaceID: workspaceID,
		Lower:       owner,
		Lower_2:     repo,
	})
}
