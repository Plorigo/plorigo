package services

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

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in the
// module allowed to import internal/platform/database/db — depguard enforces it (see
// .golangci.yml). It reads a few sibling tables (environments, projects, servers,
// source_connections) for ancestor/connection resolution, which modules.md Rule 2 permits
// from a module's postgres.go.
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) InsertService(ctx context.Context, tx database.Tx, w ServiceWrite) (Service, error) {
	row, err := db.New(tx).CreateService(ctx, db.CreateServiceParams{
		EnvironmentID: w.EnvironmentID,
		ProjectID:     w.ProjectID,
		WorkspaceID:   w.WorkspaceID,
		Name:          w.Name,
		Slug:          w.Slug,
		SourceKind:    w.SourceKind,
		ImageRef:      w.ImageRef,
		TemplateID:    w.TemplateID,
		ContainerPort: w.ContainerPort,
		Visibility:    w.Visibility,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Service{}, problem.AlreadyExists("a service named %q already exists in this environment", w.Name)
		}
		return Service{}, err
	}
	return serviceFromRow(row), nil
}

func (s *postgresStore) InsertGitService(ctx context.Context, tx database.Tx, w GitServiceWrite) (Service, error) {
	row, err := db.New(tx).CreateGitService(ctx, db.CreateGitServiceParams{
		EnvironmentID: w.EnvironmentID,
		ProjectID:     w.ProjectID,
		WorkspaceID:   w.WorkspaceID,
		Name:          w.Name,
		Slug:          w.Slug,
		SourceAccess:  w.SourceAccess,
		ConnectionID:  nullableStr(w.ConnectionID),
		Provider:      w.Provider,
		Owner:         w.Owner,
		Repo:          w.Repo,
		FullName:      w.FullName,
		Branch:        w.Branch,
		DefaultBranch: w.DefaultBranch,
		IsPrivate:     w.IsPrivate,
		HtmlUrl:       w.HTMLURL,
		ContainerPort: w.ContainerPort,
		Visibility:    w.Visibility,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Service{}, problem.AlreadyExists("a service named %q already exists in this environment", w.Name)
		}
		return Service{}, err
	}
	return serviceFromRow(row), nil
}

func (s *postgresStore) GetService(ctx context.Context, id string) (Service, bool, error) {
	row, err := db.New(s.pool).GetService(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Service{}, false, nil
		}
		return Service{}, false, err
	}
	return serviceFromRow(row), true, nil
}

func (s *postgresStore) ListByEnvironment(ctx context.Context, environmentID string) ([]Service, error) {
	rows, err := db.New(s.pool).ListServicesByEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	return servicesFromRows(rows), nil
}

func (s *postgresStore) ListByProject(ctx context.Context, projectID string) ([]Service, error) {
	rows, err := db.New(s.pool).ListServicesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return servicesFromRows(rows), nil
}

func (s *postgresStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Service, error) {
	rows, err := db.New(s.pool).ListServicesByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return servicesFromRows(rows), nil
}

func (s *postgresStore) UpdateServiceSource(ctx context.Context, tx database.Tx, w SourceWrite) (Service, error) {
	row, err := db.New(tx).UpdateServiceSource(ctx, db.UpdateServiceSourceParams{
		ID:            w.ID,
		SourceKind:    w.SourceKind,
		ImageRef:      w.ImageRef,
		TemplateID:    w.TemplateID,
		ConnectionID:  nullableStr(w.ConnectionID),
		Provider:      w.Provider,
		Owner:         w.Owner,
		Repo:          w.Repo,
		FullName:      w.FullName,
		Branch:        w.Branch,
		DefaultBranch: w.DefaultBranch,
		IsPrivate:     w.IsPrivate,
		HtmlUrl:       w.HTMLURL,
		SourceAccess:  w.SourceAccess,
		ContainerPort: w.ContainerPort,
	})
	if err != nil {
		return Service{}, err
	}
	return serviceFromRow(row), nil
}

func (s *postgresStore) UpdateVisibility(ctx context.Context, tx database.Tx, id, visibility string) (Service, error) {
	row, err := db.New(tx).UpdateServiceVisibility(ctx, db.UpdateServiceVisibilityParams{ID: id, Visibility: visibility})
	if err != nil {
		return Service{}, err
	}
	return serviceFromRow(row), nil
}

func (s *postgresStore) DeleteService(ctx context.Context, tx database.Tx, id string) (string, bool, error) {
	deletedID, err := db.New(tx).DeleteService(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return deletedID, true, nil
}

func (s *postgresStore) WorkspaceAndProjectForEnvironment(ctx context.Context, environmentID string) (string, string, bool, error) {
	row, err := db.New(s.pool).GetEnvironmentWorkspaceAndProject(ctx, environmentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return row.WorkspaceID, row.ProjectID, true, nil
}

func (s *postgresStore) WorkspaceAndProjectForService(ctx context.Context, serviceID string) (string, string, bool, error) {
	row, err := db.New(s.pool).GetServiceWorkspaceAndProject(ctx, serviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return row.WorkspaceID, row.ProjectID, true, nil
}

func (s *postgresStore) WorkspaceForServer(ctx context.Context, serverID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetServerWorkspace(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}

func (s *postgresStore) WorkspaceForProject(ctx context.Context, projectID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetProjectWorkspaceID(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}

func (s *postgresStore) GetConnection(ctx context.Context, workspaceID string) (string, string, bool, error) {
	row, err := db.New(s.pool).GetSourceConnectionByWorkspace(ctx, db.GetSourceConnectionByWorkspaceParams{
		WorkspaceID: workspaceID,
		Provider:    provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return row.ID, row.GithubLogin, true, nil
}

func (s *postgresStore) GetConnectionToken(ctx context.Context, workspaceID string) ([]byte, bool, error) {
	cipher, err := db.New(s.pool).GetConnectionTokenByWorkspace(ctx, db.GetConnectionTokenByWorkspaceParams{
		WorkspaceID: workspaceID,
		Provider:    provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return cipher, true, nil
}

func servicesFromRows(rows []db.Service) []Service {
	out := make([]Service, 0, len(rows))
	for _, r := range rows {
		out = append(out, serviceFromRow(r))
	}
	return out
}

func serviceFromRow(r db.Service) Service {
	return Service{
		ID:            r.ID,
		EnvironmentID: r.EnvironmentID,
		ProjectID:     r.ProjectID,
		WorkspaceID:   r.WorkspaceID,
		Name:          r.Name,
		Slug:          r.Slug,
		SourceKind:    r.SourceKind,
		ImageRef:      r.ImageRef,
		TemplateID:    r.TemplateID,
		ConnectionID:  deref(r.ConnectionID),
		Provider:      r.Provider,
		Owner:         r.Owner,
		Repo:          r.Repo,
		FullName:      r.FullName,
		Branch:        r.Branch,
		DefaultBranch: r.DefaultBranch,
		IsPrivate:     r.IsPrivate,
		HTMLURL:       r.HtmlUrl,
		SourceAccess:  r.SourceAccess,
		ContainerPort: r.ContainerPort,
		Visibility:    r.Visibility,
		RouteURL:      r.RouteUrl,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

// nullableStr maps an empty string to a NULL uuid (public sources have no connection).
func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}
