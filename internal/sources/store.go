package sources

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// OAuthConnectionWrite is the data InsertOAuthConnection persists. The token is ciphertext only.
type OAuthConnectionWrite struct {
	WorkspaceID  string
	Provider     string
	AccountLogin string
	AccountID    *int64
	TokenCipher  []byte
	Scopes       string
	ConnectedBy  *string
}

// AppConnectionWrite is the data InsertAppConnection persists for an App installation. No token —
// per-installation tokens are minted on demand from the App's private key.
type AppConnectionWrite struct {
	WorkspaceID    string
	Provider       string
	AccountLogin   string
	AccountID      *int64
	InstallationID string
	ConnectedBy    *string
}

// Store is the repository port. Connections are addressed by id (a workspace has many). Mutations
// take a database.Tx so they commit with the audit row. The store only ever sees the sealed token —
// the service seals before calling it and opens the ciphertext it reads back.
type Store interface {
	// ListConnectionsByWorkspace returns all of a workspace's connections (metadata only), newest
	// first.
	ListConnectionsByWorkspace(ctx context.Context, workspaceID string) ([]Connection, error)
	// GetConnectionByID returns one connection's metadata (never the token). ok is false (nil error)
	// when it does not exist.
	GetConnectionByID(ctx context.Context, connectionID string) (Connection, bool, error)
	// GetSealedTokenByConnection returns the sealed OAuth token for a connection, for server-side
	// provider calls. ok is false when the connection has no token (app) or does not exist.
	GetSealedTokenByConnection(ctx context.Context, connectionID string) (ciphertext []byte, ok bool, err error)
	// InsertOAuthConnection adds (or refreshes, by account) an OAuth connection.
	InsertOAuthConnection(ctx context.Context, tx database.Tx, c OAuthConnectionWrite) (Connection, error)
	// InsertAppConnection adds (or refreshes, by installation) an App-installation connection.
	InsertAppConnection(ctx context.Context, tx database.Tx, c AppConnectionWrite) (Connection, error)
	// DeleteConnectionByID removes one connection. ok is false when no row matched.
	DeleteConnectionByID(ctx context.Context, tx database.Tx, connectionID string) (deletedID string, ok bool, err error)
	// CountServicesByConnection guards disconnect: a connection still used by services (which fold the
	// source onto their row) must not be removed. Reads the services table (modules.md Rule 2).
	CountServicesByConnection(ctx context.Context, connectionID string) (int64, error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; a port for unit-testability.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for audit. *audit.Service satisfies it structurally.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// SecretBox is the CONSUMER-DEFINED port for sealing/opening the OAuth token at rest. *crypto.Box
// satisfies it. The token is opened only in-process to call the provider; never returned or logged.
type SecretBox interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}
