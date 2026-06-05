// Package auth owns identity: email/password users, browser sessions, API tokens
// for the CLI/agent, and the email-verification / password-reset flows. It exposes
// the AuthService RPC surface and the resolvers the app's auth interceptor uses to
// turn a cookie or bearer token into a principal. Authorization is NOT here — that
// is the policy module. See docs/architecture/auth.md.
package auth

import (
	"context"
	"time"

	"github.com/plorigo/plorigo/internal/platform/principal"
)

// User is the identity domain model (no password material).
type User struct {
	ID            string
	Email         string
	EmailVerified bool
	CreatedAt     time.Time
}

// APIToken is the non-secret metadata of an API token (never the raw value).
type APIToken struct {
	ID          string
	Name        string
	TokenPrefix string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	ExpiresAt   *time.Time
}

// Config tunes session/token lifetimes and the registration policy.
type Config struct {
	// BaseURL is the dashboard origin used to build email links (verify/reset).
	BaseURL string
	// SessionTTL is the browser session lifetime; zero means the default.
	SessionTTL time.Duration
	// AllowOpenRegistration lets anyone register; when false only the first
	// (bootstrap) user and invited users may.
	AllowOpenRegistration bool
	// RequireEmailVerification sends a verification email on registration.
	RequireEmailVerification bool
}

// RegisterInput is the data needed to register a user.
type RegisterInput struct {
	Email     string
	Password  string
	UserAgent string
}

// LoginInput is the data needed to log a user in.
type LoginInput struct {
	Email     string
	Password  string
	UserAgent string
}

// Authenticated is the result of register/login: the user plus the raw session
// token the handler sets as the session cookie.
type Authenticated struct {
	User         User
	SessionToken string
}

// NewAPIToken is the result of creating an API token: the raw token (shown once)
// and its stored metadata.
type NewAPIToken struct {
	Token string
	Meta  APIToken
}

// Service is the surface the handler, the app interceptor, and tests depend on.
type Service interface {
	Register(ctx context.Context, in RegisterInput) (Authenticated, error)
	Login(ctx context.Context, in LoginInput) (Authenticated, error)
	Logout(ctx context.Context, sessionToken string) error
	CurrentUser(ctx context.Context, userID string) (User, error)

	RequestPasswordReset(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, token, newPassword string) error
	RequestEmailVerification(ctx context.Context, userID string) error
	VerifyEmail(ctx context.Context, token string) error

	CreateAPIToken(ctx context.Context, userID, name string) (NewAPIToken, error)
	ListAPITokens(ctx context.Context, userID string) ([]APIToken, error)
	RevokeAPIToken(ctx context.Context, userID, tokenID string) error

	// Resolvers used by the auth interceptor. Each returns the anonymous zero
	// principal (with a nil error) when the credential is absent, invalid, or
	// expired, so the caller can treat "no/invalid credential" as unauthenticated.
	ResolveSession(ctx context.Context, sessionToken string) (principal.Principal, error)
	ResolveAPIToken(ctx context.Context, bearer string) (principal.Principal, error)
}
