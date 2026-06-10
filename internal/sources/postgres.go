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

func (s *postgresStore) CountProjectSourcesByConnection(ctx context.Context, connectionID string) (int64, error) {
	return db.New(s.pool).CountProjectSourcesByConnection(ctx, connectionID)
}

func (s *postgresStore) UpsertProjectSource(ctx context.Context, tx database.Tx, w ProjectSourceWrite) (Source, error) {
	row, err := db.New(tx).UpsertProjectSource(ctx, db.UpsertProjectSourceParams{
		ProjectID:     w.ProjectID,
		ConnectionID:  w.ConnectionID,
		Provider:      w.Provider,
		Owner:         w.Owner,
		Repo:          w.Repo,
		FullName:      w.FullName,
		Branch:        w.Branch,
		DefaultBranch: w.DefaultBranch,
		IsPrivate:     w.IsPrivate,
		HtmlUrl:       w.HTMLURL,
	})
	if err != nil {
		return Source{}, err
	}
	// GithubLogin is not in this RETURNING (it lives on the connection); the service
	// sets it from the connection it already loaded.
	return Source{
		ID:            row.ID,
		ProjectID:     row.ProjectID,
		ConnectionID:  row.ConnectionID,
		Provider:      row.Provider,
		Owner:         row.Owner,
		Repo:          row.Repo,
		FullName:      row.FullName,
		Branch:        row.Branch,
		DefaultBranch: row.DefaultBranch,
		IsPrivate:     row.IsPrivate,
		HTMLURL:       row.HtmlUrl,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

func (s *postgresStore) GetProjectSource(ctx context.Context, projectID string) (Source, bool, error) {
	row, err := db.New(s.pool).GetProjectSource(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Source{}, false, nil
		}
		return Source{}, false, err
	}
	return Source{
		ID:            row.ID,
		ProjectID:     row.ProjectID,
		ConnectionID:  row.ConnectionID,
		Provider:      row.Provider,
		Owner:         row.Owner,
		Repo:          row.Repo,
		FullName:      row.FullName,
		Branch:        row.Branch,
		DefaultBranch: row.DefaultBranch,
		IsPrivate:     row.IsPrivate,
		HTMLURL:       row.HtmlUrl,
		GitHubLogin:   row.GithubLogin,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}, true, nil
}

func (s *postgresStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Source, error) {
	rows, err := db.New(s.pool).ListProjectSourcesByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Source, 0, len(rows))
	for _, r := range rows {
		out = append(out, Source{
			ID:            r.ID,
			ProjectID:     r.ProjectID,
			ConnectionID:  r.ConnectionID,
			WorkspaceID:   workspaceID,
			Provider:      r.Provider,
			Owner:         r.Owner,
			Repo:          r.Repo,
			FullName:      r.FullName,
			Branch:        r.Branch,
			DefaultBranch: r.DefaultBranch,
			IsPrivate:     r.IsPrivate,
			HTMLURL:       r.HtmlUrl,
			GitHubLogin:   r.GithubLogin,
			CreatedAt:     r.CreatedAt,
			UpdatedAt:     r.UpdatedAt,
		})
	}
	return out, nil
}

func (s *postgresStore) DeleteProjectSource(ctx context.Context, tx database.Tx, projectID string) (string, bool, error) {
	id, err := db.New(tx).DeleteProjectSource(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

// WorkspaceIDForProject reuses the shared project->workspace resolution query.
func (s *postgresStore) WorkspaceIDForProject(ctx context.Context, projectID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetProjectWorkspaceID(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}
