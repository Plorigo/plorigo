package backups

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in the
// module allowed to import internal/platform/database/db — depguard enforces it. It reads a few
// sibling tables (services, deployments, agents, config_entries) for target/credential
// resolution, which modules.md Rule 2 permits from a module's postgres.go.
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) InsertBackup(ctx context.Context, tx database.Tx, b NewBackup) (Backup, error) {
	row, err := db.New(tx).CreateBackup(ctx, db.CreateBackupParams{
		ServiceID:     b.ServiceID,
		EnvironmentID: b.EnvironmentID,
		ProjectID:     b.ProjectID,
		WorkspaceID:   b.WorkspaceID,
		ServerID:      b.ServerID,
	})
	if err != nil {
		return Backup{}, err
	}
	return backupFromRow(row), nil
}

func (s *postgresStore) GetBackup(ctx context.Context, id string) (Backup, bool, error) {
	row, err := db.New(s.pool).GetBackup(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, false, nil
		}
		return Backup{}, false, err
	}
	return backupFromRow(row), true, nil
}

func (s *postgresStore) ListByService(ctx context.Context, serviceID string) ([]Backup, error) {
	rows, err := db.New(s.pool).ListBackupsByService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]Backup, 0, len(rows))
	for _, r := range rows {
		out = append(out, backupFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) ClaimNextForServer(ctx context.Context, tx database.Tx, serverID string) (Backup, bool, error) {
	row, err := db.New(tx).ClaimNextBackupForServer(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, false, nil
		}
		return Backup{}, false, err
	}
	return backupFromRow(row), true, nil
}

func (s *postgresStore) UpdateStatus(ctx context.Context, tx database.Tx, u StatusUpdate) (Backup, error) {
	row, err := db.New(tx).UpdateBackupStatus(ctx, db.UpdateBackupStatusParams{
		Status:      u.Status,
		Message:     u.Message,
		Error:       u.Error,
		ArtifactUri: u.ArtifactURI,
		SizeBytes:   u.SizeBytes,
		Checksum:    u.Checksum,
		ID:          u.BackupID,
	})
	if err != nil {
		return Backup{}, err
	}
	return backupFromRow(row), nil
}

func (s *postgresStore) ServiceTarget(ctx context.Context, serviceID string) (ServiceTarget, bool, error) {
	row, err := db.New(s.pool).GetBackupServiceTarget(ctx, serviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ServiceTarget{}, false, nil
		}
		return ServiceTarget{}, false, err
	}
	return ServiceTarget{
		ID:            row.ID,
		Name:          row.Name,
		EnvironmentID: row.EnvironmentID,
		ProjectID:     row.ProjectID,
		WorkspaceID:   row.WorkspaceID,
		SourceKind:    row.SourceKind,
		TemplateID:    row.TemplateID,
	}, true, nil
}

func (s *postgresStore) RunningServerForService(ctx context.Context, serviceID string) (string, bool, error) {
	serverID, err := db.New(s.pool).GetLatestRunningServerForService(ctx, serviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return serverID, true, nil
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

func (s *postgresStore) DBCredentialsForService(ctx context.Context, serviceID string) (DBCredentials, error) {
	rows, err := db.New(s.pool).ListConfigForService(ctx, &serviceID)
	if err != nil {
		return DBCredentials{}, err
	}
	var c DBCredentials
	for _, r := range rows {
		// Managed-database credentials are plaintext config variables (POSTGRES_*) written at
		// provision time; a secret entry (encrypted, blank value) is never one of these.
		if r.Type != "variable" || r.Value == nil {
			continue
		}
		switch r.Key {
		case "POSTGRES_USER":
			c.User = *r.Value
		case "POSTGRES_PASSWORD":
			c.Password = *r.Value
		case "POSTGRES_DB":
			c.Database = *r.Value
		}
	}
	return c, nil
}

func (s *postgresStore) InsertRestore(ctx context.Context, tx database.Tx, r NewRestore) (RestoreJob, error) {
	row, err := db.New(tx).CreateRestoreJob(ctx, db.CreateRestoreJobParams{
		BackupID:      r.BackupID,
		ServiceID:     r.ServiceID,
		EnvironmentID: r.EnvironmentID,
		ProjectID:     r.ProjectID,
		WorkspaceID:   r.WorkspaceID,
		ServerID:      r.ServerID,
		ArtifactUri:   r.ArtifactURI,
	})
	if err != nil {
		return RestoreJob{}, err
	}
	return restoreFromRow(row), nil
}

func (s *postgresStore) GetRestore(ctx context.Context, id string) (RestoreJob, bool, error) {
	row, err := db.New(s.pool).GetRestoreJob(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RestoreJob{}, false, nil
		}
		return RestoreJob{}, false, err
	}
	return restoreFromRow(row), true, nil
}

func (s *postgresStore) ListRestoresByService(ctx context.Context, serviceID string) ([]RestoreJob, error) {
	rows, err := db.New(s.pool).ListRestoreJobsByService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]RestoreJob, 0, len(rows))
	for _, r := range rows {
		out = append(out, restoreFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) ClaimNextRestoreForServer(ctx context.Context, tx database.Tx, serverID string) (RestoreJob, bool, error) {
	row, err := db.New(tx).ClaimNextRestoreForServer(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RestoreJob{}, false, nil
		}
		return RestoreJob{}, false, err
	}
	return restoreFromRow(row), true, nil
}

func (s *postgresStore) UpdateRestoreStatus(ctx context.Context, tx database.Tx, u RestoreStatusUpdate) (RestoreJob, error) {
	row, err := db.New(tx).UpdateRestoreStatus(ctx, db.UpdateRestoreStatusParams{
		Status:  u.Status,
		Message: u.Message,
		Error:   u.Error,
		ID:      u.RestoreID,
	})
	if err != nil {
		return RestoreJob{}, err
	}
	return restoreFromRow(row), nil
}

func restoreFromRow(r db.RestoreJob) RestoreJob {
	return RestoreJob{
		ID:            r.ID,
		BackupID:      r.BackupID,
		ServiceID:     r.ServiceID,
		EnvironmentID: r.EnvironmentID,
		ProjectID:     r.ProjectID,
		WorkspaceID:   r.WorkspaceID,
		ServerID:      r.ServerID,
		ArtifactURI:   r.ArtifactUri,
		Status:        r.Status,
		Message:       r.Message,
		Error:         r.Error,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

func backupFromRow(r db.Backup) Backup {
	return Backup{
		ID:            r.ID,
		ServiceID:     r.ServiceID,
		EnvironmentID: r.EnvironmentID,
		ProjectID:     r.ProjectID,
		WorkspaceID:   r.WorkspaceID,
		ServerID:      r.ServerID,
		Destination:   r.Destination,
		ArtifactURI:   r.ArtifactUri,
		SizeBytes:     r.SizeBytes,
		Checksum:      r.Checksum,
		Status:        r.Status,
		Message:       r.Message,
		Error:         r.Error,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}
