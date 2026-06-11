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

// ProjectSourceWrite is the data UpsertProjectSource persists. ConnectionID is empty for
// a public source (no connection); Access records how the source is reached.
type ProjectSourceWrite struct {
	ProjectID     string
	ConnectionID  string
	Provider      string
	Owner         string
	Repo          string
	FullName      string
	Branch        string
	DefaultBranch string
	IsPrivate     bool
	HTMLURL       string
	Access        string
}

// Store is the repository port the service needs. Implemented by postgres.go, faked in
// tests. Mutations take a database.Tx so they commit with the audit row. The store only
// ever sees the sealed token — the service seals before calling it and opens after
// reading the ciphertext back.
type Store interface {
	UpsertConnection(ctx context.Context, tx database.Tx, c ConnectionWrite) (Connection, error)
	// GetConnection returns the workspace's connection metadata (never the token). ok is
	// false (nil error) when there is no connection.
	GetConnection(ctx context.Context, workspaceID, provider string) (Connection, bool, error)
	// GetConnectionToken returns the sealed token for server-side provider calls. ok is
	// false (nil error) when there is no connection.
	GetConnectionToken(ctx context.Context, workspaceID, provider string) (ciphertext []byte, ok bool, err error)
	// DeleteConnection removes the workspace's connection. ok is false when no row matched.
	DeleteConnection(ctx context.Context, tx database.Tx, workspaceID, provider string) (deletedID string, ok bool, err error)
	// CountProjectSourcesByConnection guards DisconnectGitHub.
	CountProjectSourcesByConnection(ctx context.Context, connectionID string) (int64, error)

	UpsertProjectSource(ctx context.Context, tx database.Tx, s ProjectSourceWrite) (Source, error)
	// GetProjectSource returns a project's source. ok is false when none is connected.
	GetProjectSource(ctx context.Context, projectID string) (Source, bool, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]Source, error)
	// DeleteProjectSource removes a project's source. ok is false when no row matched.
	DeleteProjectSource(ctx context.Context, tx database.Tx, projectID string) (deletedID string, ok bool, err error)

	// WorkspaceIDForProject resolves a project's owning workspace, so this
	// project-scoped module can authorize and audit against the workspace. ok is false
	// (nil error) when the project does not exist.
	WorkspaceIDForProject(ctx context.Context, projectID string) (workspaceID string, ok bool, err error)
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
	GetRepository(ctx context.Context, token, owner, repo string) (github.RepoInfo, error)
	ListUserRepos(ctx context.Context, token string, opts github.ListReposOptions) ([]github.RepoInfo, error)
	ListBranches(ctx context.Context, token, owner, repo string) ([]string, error)
	GetBranch(ctx context.Context, token, owner, repo, branch string) error
	RevokeToken(ctx context.Context, clientID, clientSecret, token string) error
}
