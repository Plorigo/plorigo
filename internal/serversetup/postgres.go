package serversetup

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in
// the module allowed to import internal/platform/database/db — depguard enforces it.
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) Upsert(ctx context.Context, tx database.Tx, p UpsertParams) (Credential, error) {
	row, err := db.New(tx).UpsertSSHManagementKey(ctx, db.UpsertSSHManagementKeyParams{
		ServerID:         p.ServerID,
		Fingerprint:      p.Fingerprint,
		PublicKey:        p.PublicKey,
		SealedPrivateKey: p.SealedPrivateKey,
		CreatedBy:        p.CreatedBy,
	})
	if err != nil {
		return Credential{}, err
	}
	return toCredential(row.ID, row.ServerID, row.Fingerprint, row.PublicKey, row.RotationState,
		row.LastUsedAt, row.RotatedAt, row.RevokedAt, row.CreatedBy, row.CreatedAt, row.UpdatedAt), nil
}

func (s *postgresStore) Rotate(ctx context.Context, tx database.Tx, p RotateParams) (Credential, bool, error) {
	row, err := db.New(tx).RotateSSHManagementKey(ctx, db.RotateSSHManagementKeyParams{
		ServerID:         p.ServerID,
		Fingerprint:      p.Fingerprint,
		PublicKey:        p.PublicKey,
		SealedPrivateKey: p.SealedPrivateKey,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Credential{}, false, nil
		}
		return Credential{}, false, err
	}
	return toCredential(row.ID, row.ServerID, row.Fingerprint, row.PublicKey, row.RotationState,
		row.LastUsedAt, row.RotatedAt, row.RevokedAt, row.CreatedBy, row.CreatedAt, row.UpdatedAt), true, nil
}

func (s *postgresStore) Revoke(ctx context.Context, tx database.Tx, serverID string) (string, bool, error) {
	revokedID, err := db.New(tx).RevokeSSHManagementKey(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return revokedID, true, nil
}

func (s *postgresStore) MarkUsed(ctx context.Context, tx database.Tx, serverID string) (string, bool, error) {
	usedID, err := db.New(tx).MarkSSHManagementKeyUsed(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return usedID, true, nil
}

func (s *postgresStore) Get(ctx context.Context, serverID string) (Credential, bool, error) {
	row, err := db.New(s.pool).GetSSHManagementKey(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Credential{}, false, nil
		}
		return Credential{}, false, err
	}
	return toCredential(row.ID, row.ServerID, row.Fingerprint, row.PublicKey, row.RotationState,
		row.LastUsedAt, row.RotatedAt, row.RevokedAt, row.CreatedBy, row.CreatedAt, row.UpdatedAt), true, nil
}

func (s *postgresStore) GetSealed(ctx context.Context, serverID string) ([]byte, bool, error) {
	sealed, err := db.New(s.pool).GetSealedSSHManagementKey(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return sealed, true, nil
}

// WorkspaceIDForServer reuses the shared server->workspace resolution query (a server
// belongs directly to one workspace), so this module authorizes and audits against it.
func (s *postgresStore) WorkspaceIDForServer(ctx context.Context, serverID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetServerWorkspace(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}

// --- Dashboard-managed setup runs ---

func (s *postgresStore) InsertSetupRun(ctx context.Context, tx database.Tx, serverID, workspaceID string, startedBy *string) (SetupRun, error) {
	row, err := db.New(tx).InsertServerSetupRun(ctx, db.InsertServerSetupRunParams{ServerID: serverID, WorkspaceID: workspaceID, StartedBy: startedBy})
	if err != nil {
		return SetupRun{}, err
	}
	return toSetupRun(row), nil
}

func (s *postgresStore) CountSetupRuns(ctx context.Context, serverID string) (int64, error) {
	return db.New(s.pool).CountServerSetupRunsForServer(ctx, serverID)
}

func (s *postgresStore) SetSetupRunStatus(ctx context.Context, setupRunID, status, failureReason string) (SetupRun, bool, error) {
	row, err := db.New(s.pool).SetServerSetupRunStatus(ctx, db.SetServerSetupRunStatusParams{ID: setupRunID, Status: status, FailureReason: failureReason})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SetupRun{}, false, nil
		}
		return SetupRun{}, false, err
	}
	return toSetupRun(row), true, nil
}

func (s *postgresStore) GetSetupRun(ctx context.Context, setupRunID string) (SetupRun, bool, error) {
	row, err := db.New(s.pool).GetServerSetupRun(ctx, setupRunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SetupRun{}, false, nil
		}
		return SetupRun{}, false, err
	}
	return toSetupRun(row), true, nil
}

func (s *postgresStore) AppendSetupEvent(ctx context.Context, e NewSetupEvent) (SetupEvent, error) {
	row, err := db.New(s.pool).AppendServerSetupEvent(ctx, db.AppendServerSetupEventParams{
		SetupRunID: e.SetupRunID, Step: e.Step, Kind: e.Kind, Status: e.Status, Message: e.Message,
	})
	if err != nil {
		return SetupEvent{}, err
	}
	return toSetupEvent(row), nil
}

func (s *postgresStore) ListSetupEvents(ctx context.Context, setupRunID string, afterSeq int64) ([]SetupEvent, error) {
	rows, err := db.New(s.pool).ListServerSetupEvents(ctx, db.ListServerSetupEventsParams{SetupRunID: setupRunID, Seq: afterSeq})
	if err != nil {
		return nil, err
	}
	out := make([]SetupEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, toSetupEvent(r))
	}
	return out, nil
}

func (s *postgresStore) HostKeyFingerprint(ctx context.Context, serverID string) (string, bool, error) {
	fp, err := db.New(s.pool).GetServerHostKeyFingerprint(ctx, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return fp, true, nil
}

func (s *postgresStore) SetHostKeyFingerprint(ctx context.Context, serverID, fingerprint string) error {
	return db.New(s.pool).SetServerHostKeyFingerprint(ctx, db.SetServerHostKeyFingerprintParams{ID: serverID, HostKeyFingerprint: fingerprint})
}

func toSetupRun(r db.ServerSetupRun) SetupRun {
	return SetupRun{
		ID: r.ID, ServerID: r.ServerID, WorkspaceID: r.WorkspaceID, Status: r.Status,
		FailureReason: r.FailureReason, StartedBy: r.StartedBy,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, FinishedAt: r.FinishedAt,
	}
}

func toSetupEvent(r db.ServerSetupEvent) SetupEvent {
	return SetupEvent{Seq: r.Seq, Step: r.Step, Kind: r.Kind, Status: r.Status, Message: r.Message, CreatedAt: r.CreatedAt}
}

// toCredential builds the domain value from the (identical) metadata columns the queries
// return. The sealed private key is deliberately not among them.
func toCredential(id, serverID, fingerprint, publicKey, rotationState string, lastUsedAt, rotatedAt, revokedAt *time.Time, createdBy *string, createdAt, updatedAt time.Time) Credential {
	return Credential{
		ID:            id,
		ServerID:      serverID,
		Fingerprint:   fingerprint,
		PublicKey:     publicKey,
		RotationState: rotationState,
		LastUsedAt:    lastUsedAt,
		RotatedAt:     rotatedAt,
		RevokedAt:     revokedAt,
		CreatedBy:     createdBy,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}
}
