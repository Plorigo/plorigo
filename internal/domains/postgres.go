package domains

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

const pgUniqueViolation = "23505"

type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) CreateDomain(ctx context.Context, tx database.Tx, d Domain) (Domain, error) {
	row, err := db.New(tx).CreateDomain(ctx, db.CreateDomainParams{
		ServiceID:     d.ServiceID,
		EnvironmentID: d.EnvironmentID,
		ProjectID:     d.ProjectID,
		WorkspaceID:   d.WorkspaceID,
		Hostname:      d.Hostname,
		Status:        d.Status,
		StatusMessage: d.StatusMessage,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Domain{}, problem.AlreadyExists("domain %q is already attached in this workspace", d.Hostname)
		}
		return Domain{}, err
	}
	return domainFromRow(row), nil
}

func (s *postgresStore) GetDomain(ctx context.Context, id string) (Domain, bool, error) {
	row, err := db.New(s.pool).GetDomain(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Domain{}, false, nil
		}
		return Domain{}, false, err
	}
	return domainFromRow(row), true, nil
}

func (s *postgresStore) ListByService(ctx context.Context, serviceID string) ([]Domain, error) {
	rows, err := db.New(s.pool).ListDomainsByService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, 0, len(rows))
	for _, r := range rows {
		out = append(out, domainFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) ListByProject(ctx context.Context, projectID string) ([]Domain, error) {
	rows, err := db.New(s.pool).ListDomainsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, 0, len(rows))
	for _, r := range rows {
		out = append(out, domainFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Domain, error) {
	rows, err := db.New(s.pool).ListDomainsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, 0, len(rows))
	for _, r := range rows {
		out = append(out, domainFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) UpdateVerification(ctx context.Context, tx database.Tx, id, status, message string) (Domain, error) {
	row, err := db.New(tx).UpdateDomainVerification(ctx, db.UpdateDomainVerificationParams{
		ID:            id,
		Status:        status,
		StatusMessage: message,
	})
	if err != nil {
		return Domain{}, err
	}
	return domainFromRow(row), nil
}

func (s *postgresStore) DeleteDomain(ctx context.Context, tx database.Tx, id string) (string, bool, error) {
	deletedID, err := db.New(tx).DeleteDomain(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return deletedID, true, nil
}

func (s *postgresStore) ServiceRoute(ctx context.Context, serviceID string) (ServiceRoute, bool, error) {
	row, err := db.New(s.pool).GetDomainServiceForCreate(ctx, serviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ServiceRoute{}, false, nil
		}
		return ServiceRoute{}, false, err
	}
	return ServiceRoute{
		ID:            row.ID,
		EnvironmentID: row.EnvironmentID,
		ProjectID:     row.ProjectID,
		WorkspaceID:   row.WorkspaceID,
		Visibility:    row.Visibility,
		RouteURL:      row.RouteUrl,
	}, true, nil
}

func (s *postgresStore) WorkspaceForProject(ctx context.Context, projectID string) (string, bool, error) {
	project, err := db.New(s.pool).GetProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return project.WorkspaceID, true, nil
}

func domainFromRow(r db.Domain) Domain {
	return Domain{
		ID:            r.ID,
		ServiceID:     r.ServiceID,
		EnvironmentID: r.EnvironmentID,
		ProjectID:     r.ProjectID,
		WorkspaceID:   r.WorkspaceID,
		Hostname:      r.Hostname,
		Status:        r.Status,
		StatusMessage: r.StatusMessage,
		LastCheckedAt: r.LastCheckedAt,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}
