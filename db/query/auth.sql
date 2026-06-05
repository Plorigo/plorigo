-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING id, email, email_verified, created_at;

-- name: GetUserByEmail :one
SELECT id, email, password_hash, email_verified, created_at
FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT id, email, password_hash, email_verified, created_at
FROM users
WHERE id = $1;

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: SetUserEmailVerified :exec
UPDATE users SET email_verified = true WHERE id = $1;

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, user_agent, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, token_hash, user_agent, created_at, last_used_at, expires_at, revoked_at;

-- name: GetSessionByTokenHash :one
SELECT id, user_id, token_hash, user_agent, created_at, last_used_at, expires_at, revoked_at
FROM sessions
WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now();

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = now() WHERE id = $1;

-- name: RevokeSessionByTokenHash :exec
UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllSessionsForUser :exec
UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CreateAPIToken :one
INSERT INTO api_tokens (user_id, name, token_hash, token_prefix, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, user_id, name, token_hash, token_prefix, created_at, last_used_at, expires_at, revoked_at;

-- name: GetAPITokenByHash :one
SELECT id, user_id, name, token_hash, token_prefix, created_at, last_used_at, expires_at, revoked_at
FROM api_tokens
WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now());

-- name: ListAPITokensForUser :many
SELECT id, user_id, name, token_hash, token_prefix, created_at, last_used_at, expires_at, revoked_at
FROM api_tokens
WHERE user_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = now() WHERE id = $1;

-- name: RevokeAPIToken :exec
UPDATE api_tokens SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: CreateUserToken :one
INSERT INTO user_tokens (user_id, purpose, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, purpose, token_hash, created_at, expires_at, consumed_at;

-- name: GetUserTokenByHash :one
SELECT id, user_id, purpose, token_hash, created_at, expires_at, consumed_at
FROM user_tokens
WHERE token_hash = $1 AND purpose = $2 AND consumed_at IS NULL AND expires_at > now();

-- name: ConsumeUserToken :exec
UPDATE user_tokens SET consumed_at = now() WHERE id = $1;
