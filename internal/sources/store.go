package sources

import (
	"context"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/github"
)

// ConnectionWrite is the data UpsertConnection persists. The token is ciphertext only.
type ConnectionWrite struct {
	WorkspaceID     string
	Provider        string
	GitHubLogin     string
	GitHubUserID    *int64
	TokenCiphertext []byte
	Scopes          string
	ConnectedBy     *string
}

// AppConnectionWrite is the data UpsertAppConnection persists for a GitHub App installation. There
// is no token — per-installation tokens are minted on demand from the App private key.
type AppConnectionWrite struct {
	WorkspaceID    string
	GitHubLogin    string // the installation account (user or org) login
	GitHubUserID   *int64 // the installation account id
	InstallationID string
	ConnectedBy    *string
}

// Store is the repository port the service needs. Implemented by postgres.go, faked in
// tests. Mutations take a database.Tx so they commit with the audit row. The store only
// ever sees the sealed token — the service seals before calling it and opens after
// reading the ciphertext back.
type Store interface {
	UpsertConnection(ctx context.Context, tx database.Tx, c ConnectionWrite) (Connection, error)
	// UpsertAppConnection persists a GitHub App connection (provider='github_app', installation_id,
	// no token). Reconnecting a workspace to a new installation refreshes the row.
	UpsertAppConnection(ctx context.Context, tx database.Tx, c AppConnectionWrite) (Connection, error)
	// InstallationForWorkspace returns the workspace's connected App installation id. ok is false
	// (nil error) when no App installation is connected.
	InstallationForWorkspace(ctx context.Context, workspaceID string) (installationID string, ok bool, err error)
	// GetConnection returns the workspace's connection metadata (never the token). ok is
	// false (nil error) when there is no connection.
	GetConnection(ctx context.Context, workspaceID, provider string) (Connection, bool, error)
	// GetConnectionToken returns the sealed token for server-side provider calls. ok is
	// false (nil error) when there is no connection.
	GetConnectionToken(ctx context.Context, workspaceID, provider string) (ciphertext []byte, ok bool, err error)
	// DeleteConnection removes the workspace's connection. ok is false when no row matched.
	DeleteConnection(ctx context.Context, tx database.Tx, workspaceID, provider string) (deletedID string, ok bool, err error)
	// CountServicesByConnection guards DisconnectGitHub: a connection still used by services
	// (which fold the OAuth source onto their row) must not be removed. Reads the services
	// table as a sibling-table read (modules.md Rule 2).
	CountServicesByConnection(ctx context.Context, connectionID string) (int64, error)
}

// TxRunner runs fn inside one transaction. Implemented by *database.DB; declared here as
// a port so the service is unit-testable without a database.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED port for what sources needs from the audit module.
// *audit.Service satisfies it structurally — sources never imports audit.
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// SecretBox is the CONSUMER-DEFINED port for sealing and opening the OAuth token at
// rest. *crypto.Box satisfies it structurally — sources never imports platform/crypto,
// and the master key stays in that package. Unlike secrets (Seal-only) this module also
// Opens: the token is used server-side to call the provider on the user's behalf. It is
// still never returned by any RPC or logged.
type SecretBox interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}

// GitHubClient is the CONSUMER-DEFINED port for reaching GitHub. *github.Client
// satisfies it structurally — the concrete client is wired in internal/app. It returns
// github's own DTOs and typed errors; the service maps both into domain types and
// problem.* errors.
type GitHubClient interface {
	AuthorizeURL(clientID, redirectURI, scopes, state string) string
	ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (github.Token, error)
	GetAuthenticatedUser(ctx context.Context, token string) (github.User, error)
	ListUserRepos(ctx context.Context, token string, opts github.ListReposOptions) ([]github.RepoInfo, error)
	ListBranches(ctx context.Context, token, owner, repo string) ([]string, error)
	RevokeToken(ctx context.Context, clientID, clientSecret, token string) error

	// GitHub App: resolve a new installation's account, and mint a short-lived per-installation
	// access token for server-side private-repo reads. *github.Client satisfies these.
	GetInstallation(ctx context.Context, installationID string) (github.Installation, error)
	InstallationToken(ctx context.Context, installationID string) (string, error)
}
