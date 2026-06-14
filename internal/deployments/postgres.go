package deployments

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in
// the module allowed to import internal/platform/database/db — depguard enforces it
// (see .golangci.yml). It reads a few sibling tables (environments, projects, servers,
// agents, env_vars) for ancestor/credential resolution, which modules.md Rule 2 permits
// from a module's postgres.go (a read, not a cross-module import).
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
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

func (s *postgresStore) AgentServerByCredential(ctx context.Context, credentialHash []byte) (string, string, bool, error) {
	row, err := db.New(s.pool).GetAgentServerByCredential(ctx, credentialHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, err
	}
	return row.ID, row.ServerID, true, nil
}

func (s *postgresStore) EnvVarsForEnvironment(ctx context.Context, environmentID string) (map[string]string, error) {
	rows, err := db.New(s.pool).ListEnvVarsByEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	return out, nil
}

func (s *postgresStore) SourceForProject(ctx context.Context, projectID string) (Source, bool, error) {
	row, err := db.New(s.pool).GetProjectSourceForDeploy(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Source{}, false, nil
		}
		return Source{}, false, err
	}
	return Source{
		// Provider is GitHub-only today (project_sources.provider CHECK); construct the
		// standard clone URL from owner/repo.
		CloneURL:      "https://github.com/" + row.Owner + "/" + row.Repo + ".git",
		Branch:        row.Branch,
		DefaultBranch: row.DefaultBranch,
		Access:        row.Access,
	}, true, nil
}

func (s *postgresStore) InsertDeployment(ctx context.Context, tx database.Tx, d NewDeployment) (Deployment, error) {
	row, err := db.New(tx).CreateDeployment(ctx, db.CreateDeploymentParams{
		EnvironmentID: d.EnvironmentID,
		ProjectID:     d.ProjectID,
		WorkspaceID:   d.WorkspaceID,
		ServerID:      d.ServerID,
		ImageRef:      d.ImageRef,
		ContainerPort: d.ContainerPort,
	})
	if err != nil {
		return Deployment{}, err
	}
	return deploymentFromRow(row), nil
}

func (s *postgresStore) InsertDeploymentFromGit(ctx context.Context, tx database.Tx, d NewDeploymentFromGit) (Deployment, error) {
	row, err := db.New(tx).CreateDeploymentFromGit(ctx, db.CreateDeploymentFromGitParams{
		EnvironmentID: d.EnvironmentID,
		ProjectID:     d.ProjectID,
		WorkspaceID:   d.WorkspaceID,
		ServerID:      d.ServerID,
		ContainerPort: d.ContainerPort,
		SourceAccess:  d.SourceAccess,
		CloneUrl:      d.CloneURL,
		GitRef:        d.GitRef,
	})
	if err != nil {
		return Deployment{}, err
	}
	return deploymentFromRow(row), nil
}

func (s *postgresStore) GetDeployment(ctx context.Context, deploymentID string) (Deployment, bool, error) {
	row, err := db.New(s.pool).GetDeployment(ctx, deploymentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, false, nil
		}
		return Deployment{}, false, err
	}
	return deploymentFromRow(row), true, nil
}

func (s *postgresStore) ListByEnvironment(ctx context.Context, environmentID string) ([]Deployment, error) {
	rows, err := db.New(s.pool).ListDeploymentsByEnvironment(ctx, environmentID)
	if err != nil {
		return nil, err
	}
	return deploymentsFromRows(rows), nil
}

func (s *postgresStore) ListByProject(ctx context.Context, projectID string) ([]Deployment, error) {
	rows, err := db.New(s.pool).ListDeploymentsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return deploymentsFromRows(rows), nil
}

func (s *postgresStore) ListByWorkspace(ctx context.Context, workspaceID string) ([]Deployment, error) {
	rows, err := db.New(s.pool).ListDeploymentsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return deploymentsFromRows(rows), nil
}

func (s *postgresStore) ListEvents(ctx context.Context, deploymentID string, afterSeq int64) ([]Event, error) {
	rows, err := db.New(s.pool).ListDeploymentEvents(ctx, db.ListDeploymentEventsParams{
		DeploymentID: deploymentID,
		Seq:          afterSeq,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, eventFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) ClaimNextForServer(ctx context.Context, tx database.Tx, serverID string) (Deployment, bool, error) {
	row, err := db.New(tx).ClaimNextDeploymentForServer(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, false, nil
		}
		return Deployment{}, false, err
	}
	return deploymentFromRow(row), true, nil
}

func (s *postgresStore) UpdateStatus(ctx context.Context, tx database.Tx, u StatusUpdate) error {
	_, err := db.New(tx).UpdateDeploymentStatus(ctx, db.UpdateDeploymentStatusParams{
		ID:            u.DeploymentID,
		Status:        u.Status,
		Message:       u.Message,
		HostPort:      u.HostPort,
		ContainerID:   u.ContainerID,
		CommitSha:     u.CommitSha,
		BuiltImageRef: u.BuiltImageRef,
	})
	return err
}

func (s *postgresStore) SupersedePreviousRunning(ctx context.Context, tx database.Tx, environmentID, serverID, deploymentID string) error {
	return db.New(tx).SupersedePreviousRunning(ctx, db.SupersedePreviousRunningParams{
		EnvironmentID: environmentID,
		ServerID:      serverID,
		ID:            deploymentID,
	})
}

func (s *postgresStore) AppendEvent(ctx context.Context, tx database.Tx, e NewEvent) error {
	_, err := db.New(tx).AppendDeploymentEvent(ctx, db.AppendDeploymentEventParams{
		DeploymentID: e.DeploymentID,
		Kind:         e.Kind,
		Status:       e.Status,
		Message:      e.Message,
		Stream:       e.Stream,
	})
	return err
}

func deploymentsFromRows(rows []db.Deployment) []Deployment {
	out := make([]Deployment, 0, len(rows))
	for _, r := range rows {
		out = append(out, deploymentFromRow(r))
	}
	return out
}

func deploymentFromRow(r db.Deployment) Deployment {
	return Deployment{
		ID:            r.ID,
		EnvironmentID: r.EnvironmentID,
		ProjectID:     r.ProjectID,
		WorkspaceID:   r.WorkspaceID,
		ServerID:      r.ServerID,
		ImageRef:      r.ImageRef,
		ContainerPort: r.ContainerPort,
		HostPort:      r.HostPort,
		ContainerID:   r.ContainerID,
		Status:        r.Status,
		Message:       r.Message,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
		SourceKind:    r.SourceKind,
		SourceAccess:  r.SourceAccess,
		CloneURL:      r.CloneUrl,
		GitRef:        r.GitRef,
		CommitSha:     r.CommitSha,
		BuiltImageRef: r.BuiltImageRef,
	}
}

func eventFromRow(r db.DeploymentEvent) Event {
	return Event{
		ID:           r.ID,
		DeploymentID: r.DeploymentID,
		Seq:          r.Seq,
		Kind:         r.Kind,
		Status:       r.Status,
		Message:      r.Message,
		Stream:       r.Stream,
		CreatedAt:    r.CreatedAt,
	}
}
