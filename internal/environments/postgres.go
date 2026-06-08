package environments

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

func (s *postgresStore) InsertEnvironment(ctx context.Context, tx database.Tx, e Environment) (Environment, error) {
	row, err := db.New(tx).CreateEnvironment(ctx, db.CreateEnvironmentParams{
		ProjectID: e.ProjectID,
		Name:      e.Name,
		Slug:      e.Slug,
		Type:      e.Type,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Environment{}, problem.AlreadyExists("an environment named %q already exists in this project", e.Name)
		}
		return Environment{}, err
	}
	return environmentFromRow(row), nil
}

func (s *postgresStore) GetEnvironment(ctx context.Context, envID string) (Environment, error) {
	row, err := db.New(s.pool).GetEnvironment(ctx, envID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Environment{}, problem.NotFound("environment %s not found", envID)
		}
		return Environment{}, err
	}
	return Environment{
		ID:          row.ID,
		ProjectID:   row.ProjectID,
		WorkspaceID: row.WorkspaceID,
		Name:        row.Name,
		Slug:        row.Slug,
		Type:        row.Type,
		CreatedAt:   row.CreatedAt,
	}, nil
}

func (s *postgresStore) ListByProject(ctx context.Context, projectID string) ([]Environment, error) {
	rows, err := db.New(s.pool).ListEnvironmentsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Environment, 0, len(rows))
	for _, r := range rows {
		out = append(out, environmentFromRow(r))
	}
	return out, nil
}

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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// environmentFromRow maps the environments-table model (no workspace_id column).
// The workspace is resolved separately for authorization/auditing.
func environmentFromRow(r db.Environment) Environment {
	return Environment{
		ID:        r.ID,
		ProjectID: r.ProjectID,
		Name:      r.Name,
		Slug:      r.Slug,
		Type:      r.Type,
		CreatedAt: r.CreatedAt,
	}
}
