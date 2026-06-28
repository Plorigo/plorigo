package backups

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/plorigo/plorigo/internal/platform/authz"
	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/id"
	"github.com/plorigo/plorigo/internal/platform/principal"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

type service struct {
	tx         TxRunner
	store      Store
	authorizer authz.Authorizer
	audit      Recorder
	log        *slog.Logger
}

func newService(tx TxRunner, store Store, authorizer authz.Authorizer, audit Recorder, log *slog.Logger) *service {
	return &service{tx: tx, store: store, authorizer: authorizer, audit: audit, log: log}
}

// CreateBackup enqueues a backup for a managed Postgres service. It authorizes before resolving
// the target's running server, then inserts the queued backup and its audit row in one tx; the
// database's server agent claims and runs it. label is an optional operator-typed name to tell
// backups apart; a dashboard-initiated backup is always triggered manually.
func (s *service) CreateBackup(ctx context.Context, serviceID, label string) (Backup, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return Backup{}, problem.InvalidInput("a valid service_id is required")
	}
	label = strings.TrimSpace(label)
	if utf8.RuneCountInString(label) > maxLabelLen {
		return Backup{}, problem.InvalidInput("the backup name must be at most %d characters", maxLabelLen)
	}
	target, ok, err := s.store.ServiceTarget(ctx, serviceID)
	if err != nil {
		return Backup{}, problem.Internalf(err, "create backup")
	}
	if !ok {
		return Backup{}, problem.NotFound("service %s not found", serviceID)
	}
	if target.SourceKind != sourceTemplate || target.TemplateID != templatePostgres {
		return Backup{}, problem.InvalidInput("backups are only supported for managed Postgres databases")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionBackupCreate,
		authz.Resource{Type: "backup", WorkspaceID: target.WorkspaceID, ID: serviceID}); err != nil {
		return Backup{}, err
	}
	serverID, running, err := s.store.RunningServerForService(ctx, serviceID)
	if err != nil {
		return Backup{}, problem.Internalf(err, "create backup")
	}
	if !running {
		return Backup{}, problem.InvalidInput("the database must be running before it can be backed up — deploy it first")
	}

	actor := principal.FromContext(ctx).UserID
	var created Backup
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		b, txErr := s.store.InsertBackup(ctx, tx, NewBackup{
			ServiceID:     target.ID,
			EnvironmentID: target.EnvironmentID,
			ProjectID:     target.ProjectID,
			WorkspaceID:   target.WorkspaceID,
			ServerID:      serverID,
			Label:         label,
			TriggerSource: TriggerManual,
		})
		if txErr != nil {
			return txErr
		}
		created = b
		return s.audit.Record(ctx, tx, string(authz.ActionBackupCreate), "backup", b.ID, target.WorkspaceID, actor)
	})
	if err != nil {
		return Backup{}, problem.Internalf(err, "create backup")
	}
	s.log.Info("backup created", "id", created.ID, "service_id", serviceID, "server_id", serverID)
	return created, nil
}

func (s *service) GetBackup(ctx context.Context, backupID string) (Backup, error) {
	if _, err := id.Parse(backupID); err != nil {
		return Backup{}, problem.InvalidInput("a valid backup id is required")
	}
	b, ok, err := s.store.GetBackup(ctx, backupID)
	if err != nil {
		return Backup{}, problem.Internalf(err, "get backup")
	}
	if !ok {
		return Backup{}, problem.NotFound("backup %s not found", backupID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionBackupRead,
		authz.Resource{Type: "backup", WorkspaceID: b.WorkspaceID, ID: b.ID}); err != nil {
		return Backup{}, err
	}
	return b, nil
}

func (s *service) ListByService(ctx context.Context, serviceID string) ([]Backup, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return nil, problem.InvalidInput("a valid service_id is required")
	}
	target, ok, err := s.store.ServiceTarget(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list backups")
	}
	if !ok {
		return nil, problem.NotFound("service %s not found", serviceID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionBackupRead,
		authz.Resource{Type: "backup", WorkspaceID: target.WorkspaceID, ID: serviceID}); err != nil {
		return nil, err
	}
	rows, err := s.store.ListByService(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list backups")
	}
	return rows, nil
}

// PollBackupJob atomically claims the next queued backup for the agent's server and resolves the
// managed database's credentials so the agent can run pg_dump. Credential-authenticated, not
// policy-authorized (like the deployment gateway).
func (s *service) PollBackupJob(ctx context.Context, in PollInput) (Claimed, error) {
	if in.Credential == "" {
		return Claimed{}, problem.InvalidInput("a credential is required")
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return Claimed{}, err
	}

	var claimed Backup
	var has bool
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		b, ok, txErr := s.store.ClaimNextForServer(ctx, tx, serverID)
		if txErr != nil {
			return txErr
		}
		if !ok {
			return nil
		}
		claimed, has = b, true
		return nil
	})
	if err != nil {
		return Claimed{}, problem.Internalf(err, "poll backup job")
	}
	if !has {
		return Claimed{HasWork: false}, nil
	}

	creds, err := s.store.DBCredentialsForService(ctx, claimed.ServiceID)
	if err != nil {
		return Claimed{}, problem.Internalf(err, "poll backup job")
	}
	s.log.Info("backup claimed", "id", claimed.ID, "service_id", claimed.ServiceID, "server_id", serverID)
	return Claimed{
		HasWork:    true,
		BackupID:   claimed.ID,
		Kind:       KindBackup,
		ServiceID:  claimed.ServiceID,
		Engine:     EnginePostgres,
		PgUser:     creds.User,
		PgPassword: creds.Password,
		PgDatabase: creds.Database,
	}, nil
}

// ReportBackupJob records a backup's status transition. It verifies the backup belongs to the
// agent's own server before writing anything.
func (s *service) ReportBackupJob(ctx context.Context, in ReportInput) error {
	if in.Credential == "" {
		return problem.InvalidInput("a credential is required")
	}
	if _, err := id.Parse(in.BackupID); err != nil {
		return problem.InvalidInput("a valid backup id is required")
	}
	if !isAgentReportableStatus(in.Status) {
		return problem.InvalidInput("status %q is not a valid agent-reported backup status", in.Status)
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return err
	}
	b, ok, err := s.store.GetBackup(ctx, in.BackupID)
	if err != nil {
		return problem.Internalf(err, "report backup job")
	}
	if !ok {
		return problem.NotFound("backup %s not found", in.BackupID)
	}
	if b.ServerID != serverID {
		return problem.PermissionDenied("this agent does not own backup %s", in.BackupID)
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		_, txErr := s.store.UpdateStatus(ctx, tx, StatusUpdate{
			BackupID:    in.BackupID,
			Status:      in.Status,
			ArtifactURI: in.ArtifactURI,
			SizeBytes:   in.SizeBytes,
			Checksum:    in.Checksum,
			Message:     in.Message,
			Error:       in.Error,
		})
		return txErr
	})
	if err != nil {
		return problem.Internalf(err, "report backup job")
	}
	return nil
}

// RestoreBackup enqueues a restore of a succeeded backup back into its database service. The
// restore runs on the SAME server the backup lives on (the artifact is on that server's disk) and
// the database must be running there to restore into it.
func (s *service) RestoreBackup(ctx context.Context, backupID string) (RestoreJob, error) {
	if _, err := id.Parse(backupID); err != nil {
		return RestoreJob{}, problem.InvalidInput("a valid backup id is required")
	}
	b, ok, err := s.store.GetBackup(ctx, backupID)
	if err != nil {
		return RestoreJob{}, problem.Internalf(err, "restore backup")
	}
	if !ok {
		return RestoreJob{}, problem.NotFound("backup %s not found", backupID)
	}
	if b.Status != StatusSucceeded {
		return RestoreJob{}, problem.InvalidInput("only a succeeded backup can be restored")
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionBackupCreate,
		authz.Resource{Type: "backup", WorkspaceID: b.WorkspaceID, ID: b.ServiceID}); err != nil {
		return RestoreJob{}, err
	}
	serverID, running, err := s.store.RunningServerForService(ctx, b.ServiceID)
	if err != nil {
		return RestoreJob{}, problem.Internalf(err, "restore backup")
	}
	if !running {
		return RestoreJob{}, problem.InvalidInput("the database must be running to restore into it — deploy it first")
	}
	if serverID != b.ServerID {
		return RestoreJob{}, problem.InvalidInput("the backup is stored on a different server than the database currently runs on")
	}

	actor := principal.FromContext(ctx).UserID
	var created RestoreJob
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		r, txErr := s.store.InsertRestore(ctx, tx, NewRestore{
			BackupID:      b.ID,
			ServiceID:     b.ServiceID,
			EnvironmentID: b.EnvironmentID,
			ProjectID:     b.ProjectID,
			WorkspaceID:   b.WorkspaceID,
			ServerID:      b.ServerID,
			ArtifactURI:   b.ArtifactURI,
		})
		if txErr != nil {
			return txErr
		}
		created = r
		return s.audit.Record(ctx, tx, "backup.restore", "restore", r.ID, b.WorkspaceID, actor)
	})
	if err != nil {
		return RestoreJob{}, problem.Internalf(err, "restore backup")
	}
	s.log.Info("restore created", "id", created.ID, "backup_id", b.ID, "service_id", b.ServiceID, "server_id", b.ServerID)
	return created, nil
}

func (s *service) ListRestoresByService(ctx context.Context, serviceID string) ([]RestoreJob, error) {
	if _, err := id.Parse(serviceID); err != nil {
		return nil, problem.InvalidInput("a valid service_id is required")
	}
	target, ok, err := s.store.ServiceTarget(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list restores")
	}
	if !ok {
		return nil, problem.NotFound("service %s not found", serviceID)
	}
	if err := s.authorizer.Authorize(ctx, principal.FromContext(ctx), authz.ActionBackupRead,
		authz.Resource{Type: "backup", WorkspaceID: target.WorkspaceID, ID: serviceID}); err != nil {
		return nil, err
	}
	rows, err := s.store.ListRestoresByService(ctx, serviceID)
	if err != nil {
		return nil, problem.Internalf(err, "list restores")
	}
	return rows, nil
}

// PollRestoreJob claims the next queued restore for the agent's server and resolves the target
// database's credentials + the source artifact path. Credential-authenticated.
func (s *service) PollRestoreJob(ctx context.Context, in PollInput) (ClaimedRestore, error) {
	if in.Credential == "" {
		return ClaimedRestore{}, problem.InvalidInput("a credential is required")
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return ClaimedRestore{}, err
	}
	var claimed RestoreJob
	var has bool
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		r, ok, txErr := s.store.ClaimNextRestoreForServer(ctx, tx, serverID)
		if txErr != nil {
			return txErr
		}
		if !ok {
			return nil
		}
		claimed, has = r, true
		return nil
	})
	if err != nil {
		return ClaimedRestore{}, problem.Internalf(err, "poll restore job")
	}
	if !has {
		return ClaimedRestore{HasWork: false}, nil
	}
	creds, err := s.store.DBCredentialsForService(ctx, claimed.ServiceID)
	if err != nil {
		return ClaimedRestore{}, problem.Internalf(err, "poll restore job")
	}
	s.log.Info("restore claimed", "id", claimed.ID, "service_id", claimed.ServiceID, "server_id", serverID)
	return ClaimedRestore{
		HasWork:     true,
		RestoreID:   claimed.ID,
		ServiceID:   claimed.ServiceID,
		Engine:      EnginePostgres,
		PgUser:      creds.User,
		PgPassword:  creds.Password,
		PgDatabase:  creds.Database,
		ArtifactURI: claimed.ArtifactURI,
	}, nil
}

func (s *service) ReportRestoreJob(ctx context.Context, in ReportRestoreInput) error {
	if in.Credential == "" {
		return problem.InvalidInput("a credential is required")
	}
	if _, err := id.Parse(in.RestoreID); err != nil {
		return problem.InvalidInput("a valid restore id is required")
	}
	if !isAgentReportableRestoreStatus(in.Status) {
		return problem.InvalidInput("status %q is not a valid agent-reported restore status", in.Status)
	}
	_, serverID, err := s.resolveAgent(ctx, in.AgentID, in.Credential)
	if err != nil {
		return err
	}
	r, ok, err := s.store.GetRestore(ctx, in.RestoreID)
	if err != nil {
		return problem.Internalf(err, "report restore job")
	}
	if !ok {
		return problem.NotFound("restore %s not found", in.RestoreID)
	}
	if r.ServerID != serverID {
		return problem.PermissionDenied("this agent does not own restore %s", in.RestoreID)
	}
	err = s.tx.WithinTx(ctx, func(tx database.Tx) error {
		_, txErr := s.store.UpdateRestoreStatus(ctx, tx, RestoreStatusUpdate{
			RestoreID: in.RestoreID,
			Status:    in.Status,
			Message:   in.Message,
			Error:     in.Error,
		})
		return txErr
	})
	if err != nil {
		return problem.Internalf(err, "report restore job")
	}
	return nil
}

func (s *service) resolveAgent(ctx context.Context, agentID, credential string) (string, string, error) {
	gotAgentID, serverID, ok, err := s.store.AgentServerByCredential(ctx, hashToken(credential))
	if err != nil {
		return "", "", problem.Internalf(err, "resolve agent")
	}
	if !ok {
		return "", "", problem.PermissionDenied("unknown agent credential")
	}
	if agentID != "" && agentID != gotAgentID {
		return "", "", problem.PermissionDenied("agent id does not match the credential")
	}
	return gotAgentID, serverID, nil
}

// isAgentReportableStatus bounds the statuses an agent may report for a backup.
func isAgentReportableStatus(status string) bool {
	switch status {
	case StatusDumping, StatusUploading, StatusVerifying, StatusSucceeded, StatusFailed:
		return true
	}
	return false
}

// isAgentReportableRestoreStatus bounds the statuses an agent may report for a restore.
func isAgentReportableRestoreStatus(status string) bool {
	switch status {
	case RestoreStatusRestoring, RestoreStatusVerifying, RestoreStatusSucceeded, RestoreStatusFailed:
		return true
	}
	return false
}

// hashToken is the one-way function applied to an agent credential before lookup (the stored
// column holds the hash, never the raw credential — same as the deployment gateway).
func hashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}
