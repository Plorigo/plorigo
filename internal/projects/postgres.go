package projects

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
const pgUniqueViolation = "23505"

// postgresStore implements Store over the shared sqlc package. This is the ONLY
// file in the module allowed to import internal/platform/database/db — depguard
// enforces it (see .golangci.yml).
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) InsertProject(ctx context.Context, tx database.Tx, p Project) (Project, error) {
	row, err := db.New(tx).CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: p.WorkspaceID,
		Name:        p.Name,
		Slug:        p.Slug,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return Project{}, problem.AlreadyExists("a project named %q already exists in this workspace", p.Name)
		}
		return Project{}, err
	}
	return projectFromRow(row), nil
}

func (s *postgresStore) GetProject(ctx context.Context, projectID string) (Project, error) {
	row, err := db.New(s.pool).GetProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, problem.NotFound("project %s not found", projectID)
		}
		return Project{}, err
	}
	return projectFromRow(row), nil
}

func (s *postgresStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Project, error) {
	rows, err := db.New(s.pool).ListProjectsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(rows))
	for _, r := range rows {
		out = append(out, projectFromRow(r))
	}
	return out, nil
}

func projectFromRow(r db.Project) Project {
	return Project{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Slug:        r.Slug,
		CreatedAt:   r.CreatedAt,
	}
}
