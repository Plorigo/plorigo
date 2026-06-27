package config

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY file in the
// module allowed to import internal/platform/database/db — depguard enforces it.
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) UpsertServiceConfig(ctx context.Context, tx database.Tx, typ Type, serviceID, key string, value *string, ciphertext []byte) (Entry, error) {
	row, err := db.New(tx).UpsertServiceConfig(ctx, db.UpsertServiceConfigParams{
		Type:       string(typ),
		ServiceID:  &serviceID,
		Key:        key,
		Value:      value,
		Ciphertext: ciphertext,
	})
	if err != nil {
		return Entry{}, err
	}
	return entryFromRow(row), nil
}

func (s *postgresStore) UpsertEnvironmentConfig(ctx context.Context, tx database.Tx, typ Type, environmentID, key string, value *string, ciphertext []byte) (Entry, error) {
	row, err := db.New(tx).UpsertEnvironmentConfig(ctx, db.UpsertEnvironmentConfigParams{
		Type:          string(typ),
		EnvironmentID: &environmentID,
		Key:           key,
		Value:         value,
		Ciphertext:    ciphertext,
	})
	if err != nil {
		return Entry{}, err
	}
	return entryFromRow(row), nil
}

func (s *postgresStore) ListForService(ctx context.Context, serviceID string) ([]Entry, error) {
	rows, err := db.New(s.pool).ListConfigForService(ctx, &serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, entryFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) DeleteServiceConfig(ctx context.Context, tx database.Tx, serviceID, key string) (string, bool, error) {
	id, err := db.New(tx).DeleteServiceConfig(ctx, db.DeleteServiceConfigParams{ServiceID: &serviceID, Key: key})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

func (s *postgresStore) DeleteEnvironmentConfig(ctx context.Context, tx database.Tx, environmentID, key string) (string, bool, error) {
	id, err := db.New(tx).DeleteEnvironmentConfig(ctx, db.DeleteEnvironmentConfigParams{EnvironmentID: &environmentID, Key: key})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return id, true, nil
}

func (s *postgresStore) WorkspaceIDForService(ctx context.Context, serviceID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetServiceWorkspaceID(ctx, serviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}

func (s *postgresStore) WorkspaceIDForEnvironment(ctx context.Context, environmentID string) (string, bool, error) {
	workspaceID, err := db.New(s.pool).GetEnvironmentWorkspaceID(ctx, environmentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return workspaceID, true, nil
}

// entryFromRow maps a db row to the domain Entry. Ciphertext is intentionally dropped — a
// secret value never leaves the store. WorkspaceID is filled by the service.
func entryFromRow(r db.ConfigEntry) Entry {
	e := Entry{
		ID:        r.ID,
		Type:      Type(r.Type),
		Scope:     Scope(r.Scope),
		Key:       r.Key,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.ServiceID != nil {
		e.ServiceID = *r.ServiceID
	}
	if r.EnvironmentID != nil {
		e.EnvironmentID = *r.EnvironmentID
	}
	if r.Value != nil {
		e.Value = *r.Value
	}
	return e
}
