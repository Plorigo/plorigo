package githubapp

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
)

// postgresStore implements Store over the github_app_config singleton table.
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore { return &postgresStore{pool: d.Pool} }

func (s *postgresStore) UpsertConfig(ctx context.Context, tx database.Tx, w ConfigWrite) error {
	var createdBy *string
	if w.CreatedBy != "" {
		id := w.CreatedBy
		createdBy = &id
	}
	_, err := db.New(tx).UpsertGitHubAppConfig(ctx, db.UpsertGitHubAppConfigParams{
		AppID:               w.AppID,
		AppSlug:             w.AppSlug,
		ClientID:            w.ClientID,
		SealedPrivateKey:    w.SealedPrivateKey,
		SealedWebhookSecret: w.SealedWebhookSecret,
		SealedClientSecret:  w.SealedClientSecret,
		CreatedBy:           createdBy,
	})
	return err
}

func (s *postgresStore) GetSealedConfig(ctx context.Context) (StoredConfig, bool, error) {
	row, err := db.New(s.pool).GetSealedGitHubAppConfig(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredConfig{}, false, nil
		}
		return StoredConfig{}, false, err
	}
	return StoredConfig{
		AppID:               row.AppID,
		AppSlug:             row.AppSlug,
		ClientID:            row.ClientID,
		SealedPrivateKey:    row.SealedPrivateKey,
		SealedWebhookSecret: row.SealedWebhookSecret,
		SealedClientSecret:  row.SealedClientSecret,
	}, true, nil
}

func (s *postgresStore) DeleteConfig(ctx context.Context, tx database.Tx) error {
	return db.New(tx).DeleteGitHubAppConfig(ctx)
}
