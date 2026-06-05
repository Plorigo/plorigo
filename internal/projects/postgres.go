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
		if isUniqueViolation(err) {
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

func (s *postgresStore) InsertWorkspace(ctx context.Context, tx database.Tx, name, slug string) (Workspace, error) {
	row, err := db.New(tx).CreateWorkspace(ctx, db.CreateWorkspaceParams{Name: name, Slug: slug})
	if err != nil {
		if isUniqueViolation(err) {
			return Workspace{}, problem.AlreadyExists("a workspace with that name already exists")
		}
		return Workspace{}, err
	}
	return Workspace{ID: row.ID, Name: row.Name, Slug: row.Slug, CreatedAt: row.CreatedAt}, nil
}

func (s *postgresStore) AddMember(ctx context.Context, tx database.Tx, workspaceID, userID, role string) error {
	err := db.New(tx).AddWorkspaceMember(ctx, db.AddWorkspaceMemberParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        role,
	})
	if err != nil && isUniqueViolation(err) {
		return problem.AlreadyExists("already a member of this workspace")
	}
	return err
}

func (s *postgresStore) MemberRole(ctx context.Context, workspaceID, userID string) (string, bool, error) {
	role, err := db.New(s.pool).GetWorkspaceMemberRole(ctx, db.GetWorkspaceMemberRoleParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return role, true, nil
}

func (s *postgresStore) ListWorkspacesForUser(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := db.New(s.pool).ListWorkspacesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Workspace, 0, len(rows))
	for _, r := range rows {
		out = append(out, Workspace{ID: r.ID, Name: r.Name, Slug: r.Slug, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

func (s *postgresStore) ListMembers(ctx context.Context, workspaceID string) ([]Member, error) {
	rows, err := db.New(s.pool).ListMembers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, Member{UserID: r.UserID, Email: r.Email, Role: r.Role, CreatedAt: r.CreatedAt})
	}
	return out, nil
}

func (s *postgresStore) UpdateMemberRole(ctx context.Context, tx database.Tx, workspaceID, userID, role string) error {
	return db.New(tx).UpdateMemberRole(ctx, db.UpdateMemberRoleParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Role:        role,
	})
}

func (s *postgresStore) RemoveMember(ctx context.Context, tx database.Tx, workspaceID, userID string) error {
	return db.New(tx).RemoveMember(ctx, db.RemoveMemberParams{WorkspaceID: workspaceID, UserID: userID})
}

func (s *postgresStore) UserIDByEmail(ctx context.Context, email string) (string, bool, error) {
	row, err := db.New(s.pool).GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return row.ID, true, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
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
