package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/plorigo/plorigo/internal/platform/database"
	"github.com/plorigo/plorigo/internal/platform/database/db"
	"github.com/plorigo/plorigo/internal/platform/problem"
)

// pgUniqueViolation is the Postgres SQLSTATE for a unique-constraint violation.
const pgUniqueViolation = "23505"

// Not-found sentinels. The service maps these to anonymous principals (resolvers)
// or uniform user-facing errors (login, reset), so a missing row never leaks.
var (
	errNoUser     = errors.New("auth: user not found")
	errNoSession  = errors.New("auth: session not found")
	errNoAPIToken = errors.New("auth: api token not found")
	errNoToken    = errors.New("auth: token not found")
)

// postgresStore implements Store over the shared sqlc package. This is the ONLY
// file in the module allowed to import internal/platform/database/db — depguard
// enforces it (see .golangci.yml).
type postgresStore struct {
	pool db.DBTX
}

func newPostgresStore(d *database.DB) *postgresStore {
	return &postgresStore{pool: d.Pool}
}

func (s *postgresStore) CreateUser(ctx context.Context, tx database.Tx, email, passwordHash string) (User, error) {
	row, err := db.New(tx).CreateUser(ctx, db.CreateUserParams{Email: email, PasswordHash: &passwordHash})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return User{}, problem.AlreadyExists("an account with that email already exists")
		}
		return User{}, err
	}
	return User{ID: row.ID, Email: row.Email, EmailVerified: row.EmailVerified, CreatedAt: row.CreatedAt}, nil
}

func (s *postgresStore) GetUserByEmail(ctx context.Context, email string) (storedUser, error) {
	row, err := db.New(s.pool).GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storedUser{}, errNoUser
		}
		return storedUser{}, err
	}
	return storedUser{
		User:         User{ID: row.ID, Email: row.Email, EmailVerified: row.EmailVerified, CreatedAt: row.CreatedAt},
		PasswordHash: derefStr(row.PasswordHash),
	}, nil
}

func (s *postgresStore) GetUserByID(ctx context.Context, userID string) (User, error) {
	row, err := db.New(s.pool).GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, errNoUser
		}
		return User{}, err
	}
	return User{ID: row.ID, Email: row.Email, EmailVerified: row.EmailVerified, CreatedAt: row.CreatedAt}, nil
}

func (s *postgresStore) CountUsers(ctx context.Context) (int64, error) {
	return db.New(s.pool).CountUsers(ctx)
}

func (s *postgresStore) SetPassword(ctx context.Context, tx database.Tx, userID, passwordHash string) error {
	return db.New(tx).SetUserPassword(ctx, db.SetUserPasswordParams{ID: userID, PasswordHash: &passwordHash})
}

func (s *postgresStore) SetEmailVerified(ctx context.Context, tx database.Tx, userID string) error {
	return db.New(tx).SetUserEmailVerified(ctx, userID)
}

func (s *postgresStore) CreateSession(ctx context.Context, tx database.Tx, userID string, tokenHash []byte, userAgent string, expiresAt time.Time) error {
	_, err := db.New(tx).CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		TokenHash: tokenHash,
		UserAgent: userAgent,
		ExpiresAt: expiresAt,
	})
	return err
}

func (s *postgresStore) SessionUser(ctx context.Context, tokenHash []byte) (string, error) {
	row, err := db.New(s.pool).GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errNoSession
		}
		return "", err
	}
	return row.UserID, nil
}

func (s *postgresStore) RevokeSession(ctx context.Context, tx database.Tx, tokenHash []byte) error {
	return db.New(tx).RevokeSessionByTokenHash(ctx, tokenHash)
}

func (s *postgresStore) RevokeAllSessions(ctx context.Context, tx database.Tx, userID string) error {
	return db.New(tx).RevokeAllSessionsForUser(ctx, userID)
}

func (s *postgresStore) CreateAPIToken(ctx context.Context, tx database.Tx, userID, name string, tokenHash []byte, prefix string, expiresAt *time.Time) (APIToken, error) {
	row, err := db.New(tx).CreateAPIToken(ctx, db.CreateAPITokenParams{
		UserID:      userID,
		Name:        name,
		TokenHash:   tokenHash,
		TokenPrefix: prefix,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return APIToken{}, err
	}
	return apiTokenFromRow(row), nil
}

func (s *postgresStore) APITokenUser(ctx context.Context, tokenHash []byte) (string, error) {
	row, err := db.New(s.pool).GetAPITokenByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errNoAPIToken
		}
		return "", err
	}
	return row.UserID, nil
}

func (s *postgresStore) ListAPITokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := db.New(s.pool).ListAPITokensForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]APIToken, 0, len(rows))
	for _, r := range rows {
		out = append(out, apiTokenFromRow(r))
	}
	return out, nil
}

func (s *postgresStore) RevokeAPIToken(ctx context.Context, tx database.Tx, userID, tokenID string) error {
	return db.New(tx).RevokeAPIToken(ctx, db.RevokeAPITokenParams{ID: tokenID, UserID: userID})
}

func (s *postgresStore) CreateUserToken(ctx context.Context, tx database.Tx, userID, purpose string, tokenHash []byte, expiresAt time.Time) error {
	_, err := db.New(tx).CreateUserToken(ctx, db.CreateUserTokenParams{
		UserID:    userID,
		Purpose:   purpose,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	})
	return err
}

func (s *postgresStore) GetUserToken(ctx context.Context, tokenHash []byte, purpose string) (userToken, error) {
	row, err := db.New(s.pool).GetUserTokenByHash(ctx, db.GetUserTokenByHashParams{TokenHash: tokenHash, Purpose: purpose})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return userToken{}, errNoToken
		}
		return userToken{}, err
	}
	return userToken{TokenID: row.ID, UserID: row.UserID}, nil
}

func (s *postgresStore) ConsumeUserToken(ctx context.Context, tx database.Tx, tokenID string) error {
	return db.New(tx).ConsumeUserToken(ctx, tokenID)
}

func apiTokenFromRow(r db.ApiToken) APIToken {
	return APIToken{
		ID:          r.ID,
		Name:        r.Name,
		TokenPrefix: r.TokenPrefix,
		CreatedAt:   r.CreatedAt,
		LastUsedAt:  r.LastUsedAt,
		ExpiresAt:   r.ExpiresAt,
	}
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
