package githubapp

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
)

// Store is the repository port for the singleton GitHub App config row. The sealed columns are
// write-only at this layer: Upsert takes already-sealed bytes, GetSealed returns them for in-process
// opening only, and the metadata read never includes them.
type Store interface {
	// UpsertConfig writes (or replaces) the singleton App config. The key/secret args are already
	// sealed by the service. ok-less: there is always at most one row.
	UpsertConfig(ctx context.Context, tx database.Tx, w ConfigWrite) error
	// GetSealedConfig returns the stored app id/slug/client id plus the sealed private key, webhook
	// secret, and client secret. INTERNAL — the sealed bytes are opened in-process only. ok is false
	// when no App has been registered.
	GetSealedConfig(ctx context.Context) (StoredConfig, bool, error)
	// DeleteConfig removes the stored App (e.g. to fall back to env or before re-registering).
	DeleteConfig(ctx context.Context, tx database.Tx) error
}

// ConfigWrite is the already-sealed App config to persist. CreatedBy is the registering user.
type ConfigWrite struct {
	AppID               string
	AppSlug             string
	ClientID            string
	SealedPrivateKey    []byte
	SealedWebhookSecret []byte
	SealedClientSecret  []byte
	CreatedBy           string
}

// StoredConfig is the raw stored row: non-secret metadata plus the sealed secret blobs (opened by
// the service, never leaving the process).
type StoredConfig struct {
	AppID               string
	AppSlug             string
	ClientID            string
	SealedPrivateKey    []byte
	SealedWebhookSecret []byte
	SealedClientSecret  []byte
}

// TxRunner runs fn inside one transaction (so a write commits with its audit row).
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Sealer is the CONSUMER-DEFINED port for sealing/opening the App private key + secrets at rest.
// *crypto.Box satisfies it structurally — githubapp never imports platform/crypto.
type Sealer interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}

// Recorder is the CONSUMER-DEFINED port for audit. *audit.Service satisfies it structurally.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// ManifestConverter is the CONSUMER-DEFINED port for exchanging a manifest code for the new App's
// credentials. *github.Client satisfies it structurally.
type ManifestConverter interface {
	ConvertManifest(ctx context.Context, code string) (github.ManifestConversion, error)
}

// EnvConfig is the optional operator-set App credentials from the environment. When AppID and
// PrivateKeyPEM are both present it takes precedence over any stored config.
type EnvConfig struct {
	AppID         string
	PrivateKeyPEM string
	Slug          string
	WebhookSecret string
}

func (e EnvConfig) configured() bool {
	return e.AppID != "" && e.PrivateKeyPEM != ""
}
