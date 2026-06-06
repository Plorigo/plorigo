package auth

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/platform/database"
)

// storedUser is a user plus its password hash — used only inside the module for
// login. The hash never leaves the package.
type storedUser struct {
	User
	PasswordHash string // empty if the user has no password set
}

// userToken identifies a consumed-or-not email-verify / password-reset token.
type userToken struct {
	TokenID string
	UserID  string
}

// Store is the persistence port. Mutations take a database.Tx so they commit with
// their audit row (and, on registration, with the bootstrap workspace).
type Store interface {
	CreateUser(ctx context.Context, tx database.Tx, email, passwordHash string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (storedUser, error)
	GetUserByID(ctx context.Context, userID string) (User, error)
	SetPassword(ctx context.Context, tx database.Tx, userID, passwordHash string) error
	SetEmailVerified(ctx context.Context, tx database.Tx, userID string) error

	// AcquireRegistrationLock + CountUsersTx run inside the registration transaction
	// to serialize the closed-registration bootstrap check (no TOCTOU race).
	AcquireRegistrationLock(ctx context.Context, tx database.Tx) error
	CountUsersTx(ctx context.Context, tx database.Tx) (int64, error)

	CreateSession(ctx context.Context, tx database.Tx, userID string, tokenHash []byte, userAgent string, expiresAt time.Time) error
	SessionUser(ctx context.Context, tokenHash []byte) (userID string, err error)
	RevokeSession(ctx context.Context, tx database.Tx, tokenHash []byte) error
	RevokeAllSessions(ctx context.Context, tx database.Tx, userID string) error

	CreateAPIToken(ctx context.Context, tx database.Tx, userID, name string, tokenHash []byte, prefix string, expiresAt *time.Time) (APIToken, error)
	APITokenUser(ctx context.Context, tokenHash []byte) (userID string, err error)
	ListAPITokens(ctx context.Context, userID string) ([]APIToken, error)
	RevokeAPIToken(ctx context.Context, tx database.Tx, userID, tokenID string) error

	CreateUserToken(ctx context.Context, tx database.Tx, userID, purpose string, tokenHash []byte, expiresAt time.Time) error
	GetUserToken(ctx context.Context, tokenHash []byte, purpose string) (userToken, error)
	ConsumeUserToken(ctx context.Context, tx database.Tx, tokenID string) error
}

// TxRunner runs fn inside one transaction (implemented by *database.DB).
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(tx database.Tx) error) error
}

// Recorder is the CONSUMER-DEFINED audit port (satisfied by *audit.Service).
type Recorder interface {
	Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}

// Mailer is the CONSUMER-DEFINED email port (satisfied by internal/platform/mailer).
// Sends are best-effort; a failure is logged, never surfaced to the caller, so an
// attacker cannot probe which addresses exist.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// WorkspaceBootstrapper creates the user's initial workspace and owner membership
// inside the registration transaction. Satisfied by *projects.Service — auth never
// imports projects; the boundary is wired in internal/app. The implementation must
// only ever make the given user the owner of a brand-new workspace.
type WorkspaceBootstrapper interface {
	CreateInitialWorkspace(ctx context.Context, tx database.Tx, userID, name, actor string) (workspaceID string, err error)
}
