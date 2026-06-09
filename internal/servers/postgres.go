package servers

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

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in
// the module allowed to import internal/platform/database/db — depguard enforces it (see
// .golangci.yml).
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) InsertServer(ctx context.Context, tx database.Tx, srv Server) (Server, error) {
	row, err := db.New(tx).CreateServer(ctx, db.CreateServerParams{
		WorkspaceID: srv.WorkspaceID,
		Name:        srv.Name,
		Slug:        srv.Slug,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Server{}, problem.AlreadyExists("a server named %q already exists in this workspace", srv.Name)
		}
		return Server{}, err
	}
	return serverFromRow(row), nil
}

func (s *postgresStore) GetServer(ctx context.Context, serverID string) (Server, error) {
	row, err := db.New(s.pool).GetServer(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Server{}, problem.NotFound("server %s not found", serverID)
		}
		return Server{}, err
	}
	return serverFromRow(row), nil
}

func (s *postgresStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Server, error) {
	rows, err := db.New(s.pool).ListServersByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Server, 0, len(rows))
	for _, r := range rows {
		out = append(out, serverFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) DeleteServer(ctx context.Context, tx database.Tx, id string) (bool, error) {
	if _, err := db.New(tx).DeleteServer(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

func serverFromRow(r db.Server) Server {
	return Server{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		Name:        r.Name,
		Slug:        r.Slug,
		CreatedAt:   r.CreatedAt,
	}
}
