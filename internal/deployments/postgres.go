package deployments

import (
	"context"
	"errors"
	"time"

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

func (s *postgresStore) ConfigForService(ctx context.Context, serviceID string) ([]ConfigForDeploy, error) {
	rows, err := db.New(s.pool).ListConfigForService(ctx, &serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]ConfigForDeploy, 0, len(rows))
	for _, r := range rows {
		out = append(out, ConfigForDeploy{
			Type:       r.Type,
			Scope:      r.Scope,
			Key:        r.Key,
			Value:      r.Value,
			Ciphertext: r.Ciphertext,
		})
	}
	return out, nil
}

func (s *postgresStore) ServiceForDeploy(ctx context.Context, serviceID string) (ServiceForDeploy, bool, error) {
	return s.serviceForDeploy(ctx, s.pool, serviceID)
}

func (s *postgresStore) ServiceForDeployTx(ctx context.Context, tx database.Tx, serviceID string) (ServiceForDeploy, bool, error) {
	return s.serviceForDeploy(ctx, tx, serviceID)
}

// serviceForDeploy resolves a service's source + routing facts from the services table
// through q (the pool for a committed read, or a tx to see a same-transaction insert).
func (s *postgresStore) serviceForDeploy(ctx context.Context, q db.DBTX, serviceID string) (ServiceForDeploy, bool, error) {
	row, err := db.New(q).GetServiceForDeploy(ctx, serviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ServiceForDeploy{}, false, nil
		}
		return ServiceForDeploy{}, false, err
	}
	return ServiceForDeploy{
		EnvironmentID: row.EnvironmentID,
		ProjectID:     row.ProjectID,
		WorkspaceID:   row.WorkspaceID,
		SourceKind:    row.SourceKind,
		ImageRef:      row.ImageRef,
		SourceAccess:  row.SourceAccess,
		Owner:         row.Owner,
		Repo:          row.Repo,
		Branch:        row.Branch,
		DefaultBranch: row.DefaultBranch,
		ContainerPort: row.ContainerPort,
		Visibility:    row.Visibility,
		Slug:          row.Slug,
	}, true, nil
}

func (s *postgresStore) InsertDeployment(ctx context.Context, tx database.Tx, d NewDeployment) (Deployment, error) {
	row, err := db.New(tx).CreateDeployment(ctx, db.CreateDeploymentParams{
		ServiceID: d.ServiceID,
		// A production deployment is keyed by its service id (see SupersedePreviousRunning).
		RouteKey:       d.ServiceID,
		EnvironmentID:  d.EnvironmentID,
		ProjectID:      d.ProjectID,
		WorkspaceID:    d.WorkspaceID,
		ServerID:       d.ServerID,
		ImageRef:       d.ImageRef,
		ContainerPort:  d.ContainerPort,
		RolledBackFrom: nullableID(d.RolledBackFrom),
	})
	if err != nil {
		return Deployment{}, err
	}
	return deploymentFromRow(row), nil
}

func (s *postgresStore) InsertDeploymentFromGit(ctx context.Context, tx database.Tx, d NewDeploymentFromGit) (Deployment, error) {
	row, err := db.New(tx).CreateDeploymentFromGit(ctx, db.CreateDeploymentFromGitParams{
		ServiceID:      d.ServiceID,
		RouteKey:       d.ServiceID,
		EnvironmentID:  d.EnvironmentID,
		ProjectID:      d.ProjectID,
		WorkspaceID:    d.WorkspaceID,
		ServerID:       d.ServerID,
		ContainerPort:  d.ContainerPort,
		SourceAccess:   d.SourceAccess,
		CloneUrl:       d.CloneURL,
		GitRef:         d.GitRef,
		RolledBackFrom: nullableID(d.RolledBackFrom),
	})
	if err != nil {
		return Deployment{}, err
	}
	return deploymentFromRow(row), nil
}

func (s *postgresStore) InsertPreviewDeployment(ctx context.Context, tx database.Tx, d NewPreviewDeployment) (Deployment, error) {
	row, err := db.New(tx).CreatePreviewDeployment(ctx, db.CreatePreviewDeploymentParams{
		ServiceID:       d.ServiceID,
		RouteKey:        d.RouteKey,
		EnvironmentID:   d.EnvironmentID,
		ProjectID:       d.ProjectID,
		WorkspaceID:     d.WorkspaceID,
		ServerID:        d.ServerID,
		ContainerPort:   d.ContainerPort,
		SourceAccess:    d.SourceAccess,
		CloneUrl:        d.CloneURL,
		GitRef:          d.GitRef,
		PrNumber:        d.PRNumber,
		PrUrl:           d.PRURL,
		PreviewAuthUser: d.AuthUser,
		PreviewAuthHash: d.AuthHash,
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

func (s *postgresStore) ListByService(ctx context.Context, serviceID string) ([]Deployment, error) {
	rows, err := db.New(s.pool).ListDeploymentsByService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	return deploymentsFromRows(rows), nil
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
		RouteUrl:      u.RouteURL,
	})
	return err
}

func (s *postgresStore) SupersedePreviousRunning(ctx context.Context, tx database.Tx, routeKey, serverID, deploymentID string) error {
	return db.New(tx).SupersedePreviousRunning(ctx, db.SupersedePreviousRunningParams{
		RouteKey: routeKey,
		ServerID: serverID,
		ID:       deploymentID,
	})
}

func (s *postgresStore) UpdateServiceRouteURL(ctx context.Context, tx database.Tx, serviceID, routeURL string) error {
	return db.New(tx).UpdateServiceRouteURL(ctx, db.UpdateServiceRouteURLParams{
		ID:       serviceID,
		RouteUrl: routeURL,
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

func (s *postgresStore) VerifiedDomainsForServices(ctx context.Context, serviceIDs []string) (map[string][]string, error) {
	if len(serviceIDs) == 0 {
		return map[string][]string{}, nil
	}
	rows, err := db.New(s.pool).VerifiedDomainsForServices(ctx, serviceIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(serviceIDs))
	for _, r := range rows {
		out[r.ServiceID] = append(out[r.ServiceID], r.Hostname)
	}
	return out, nil
}

func (s *postgresStore) MarkDomainsRouteSync(ctx context.Context, tx database.Tx, serviceID string, hostnames []string, status, message string) error {
	if len(hostnames) == 0 {
		return nil
	}
	return db.New(tx).MarkDomainsRouteSync(ctx, db.MarkDomainsRouteSyncParams{
		ServiceID:     serviceID,
		Column2:       hostnames,
		Status:        status,
		StatusMessage: message,
	})
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
		ID:             r.ID,
		ServiceID:      r.ServiceID,
		EnvironmentID:  r.EnvironmentID,
		ProjectID:      r.ProjectID,
		WorkspaceID:    r.WorkspaceID,
		ServerID:       r.ServerID,
		ImageRef:       r.ImageRef,
		ContainerPort:  r.ContainerPort,
		HostPort:       r.HostPort,
		ContainerID:    r.ContainerID,
		Status:         r.Status,
		Message:        r.Message,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		SourceKind:     r.SourceKind,
		SourceAccess:   r.SourceAccess,
		CloneURL:       r.CloneUrl,
		GitRef:         r.GitRef,
		CommitSha:      r.CommitSha,
		BuiltImageRef:  r.BuiltImageRef,
		RouteURL:       r.RouteUrl,
		RolledBackFrom: derefStr(r.RolledBackFrom),
		Kind:           r.Kind,
		RouteKey:       r.RouteKey,
		PRNumber:       r.PrNumber,
		PRURL:          r.PrUrl,
		AuthUser:       r.PreviewAuthUser,
		AuthHash:       r.PreviewAuthHash,
	}
}

// nullableID maps an optional id to a nullable uuid column: "" becomes NULL (nil pointer).
func nullableID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// derefStr reads a nullable column back: a NULL (nil pointer) becomes "".
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

func (s *postgresStore) InsertTeardownJob(ctx context.Context, tx database.Tx, t NewTeardownJob) (TeardownJob, error) {
	row, err := db.New(tx).CreateTeardownJob(ctx, db.CreateTeardownJobParams{
		DeploymentID:  t.DeploymentID,
		ServiceID:     t.ServiceID,
		RouteKey:      t.RouteKey,
		EnvironmentID: t.EnvironmentID,
		ProjectID:     t.ProjectID,
		WorkspaceID:   t.WorkspaceID,
		ServerID:      t.ServerID,
	})
	if err != nil {
		return TeardownJob{}, err
	}
	return teardownFromRow(row), nil
}

func (s *postgresStore) GetTeardownJob(ctx context.Context, id string) (TeardownJob, bool, error) {
	row, err := db.New(s.pool).GetTeardownJob(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TeardownJob{}, false, nil
		}
		return TeardownJob{}, false, err
	}
	return teardownFromRow(row), true, nil
}

func (s *postgresStore) ListTeardownsByService(ctx context.Context, serviceID string) ([]TeardownJob, error) {
	rows, err := db.New(s.pool).ListTeardownJobsByService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]TeardownJob, 0, len(rows))
	for _, r := range rows {
		out = append(out, teardownFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) ClaimNextTeardownForServer(ctx context.Context, tx database.Tx, serverID string) (TeardownJob, bool, error) {
	row, err := db.New(tx).ClaimNextTeardownForServer(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TeardownJob{}, false, nil
		}
		return TeardownJob{}, false, err
	}
	return teardownFromRow(row), true, nil
}

func (s *postgresStore) UpdateTeardownStatus(ctx context.Context, tx database.Tx, u TeardownStatusUpdate) (TeardownJob, error) {
	row, err := db.New(tx).UpdateTeardownStatus(ctx, db.UpdateTeardownStatusParams{
		Status:  u.Status,
		Message: u.Message,
		Error:   u.Error,
		ID:      u.TeardownID,
	})
	if err != nil {
		return TeardownJob{}, err
	}
	return teardownFromRow(row), nil
}

func (s *postgresStore) MarkPreviewTornDown(ctx context.Context, tx database.Tx, routeKey, serverID string) error {
	return db.New(tx).MarkPreviewTornDown(ctx, db.MarkPreviewTornDownParams{RouteKey: routeKey, ServerID: serverID})
}

func (s *postgresStore) LatestServerForService(ctx context.Context, serviceID string) (string, bool, error) {
	serverID, err := db.New(s.pool).GetLatestServerForService(ctx, serviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return serverID, true, nil
}

func (s *postgresStore) LatestActivePreviewByRouteKey(ctx context.Context, serviceID, routeKey string) (Deployment, bool, error) {
	row, err := db.New(s.pool).GetLatestActivePreviewByRouteKey(ctx, db.GetLatestActivePreviewByRouteKeyParams{ServiceID: serviceID, RouteKey: routeKey})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Deployment{}, false, nil
		}
		return Deployment{}, false, err
	}
	return deploymentFromRow(row), true, nil
}

func (s *postgresStore) ListExpiredPreviews(ctx context.Context, cutoff time.Time) ([]Deployment, error) {
	rows, err := db.New(s.pool).ListExpiredPreviews(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	out := make([]Deployment, 0, len(rows))
	for _, r := range rows {
		out = append(out, deploymentFromRow(r))
	}
	return out, nil
}

func teardownFromRow(r db.TeardownJob) TeardownJob {
	return TeardownJob{
		ID:            r.ID,
		DeploymentID:  r.DeploymentID,
		ServiceID:     r.ServiceID,
		RouteKey:      r.RouteKey,
		EnvironmentID: r.EnvironmentID,
		ProjectID:     r.ProjectID,
		WorkspaceID:   r.WorkspaceID,
		ServerID:      r.ServerID,
		Status:        r.Status,
		Message:       r.Message,
		Error:         r.Error,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}
