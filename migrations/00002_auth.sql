-- +goose Up
-- Identity and authorization tables (see docs/architecture/auth.md). Tokens are
-- always stored hashed (sha256, bytea); the raw value lives only in the cookie or
-- the one-time API-token response, never in the database or logs.

ALTER TABLE users
    ADD COLUMN password_hash  text,                                  -- nullable: invited-but-not-registered users have none yet
    ADD COLUMN email_verified boolean NOT NULL DEFAULT false;

-- Browser sessions. The cookie carries an opaque token; only its sha256 is stored.
CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL UNIQUE,
    user_agent   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz
);

CREATE INDEX idx_sessions_user ON sessions (user_id);

-- API tokens for the CLI/agent. The bearer value is shown once at creation.
CREATE TABLE api_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    token_prefix text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    expires_at   timestamptz,
    revoked_at   timestamptz
);

CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);

-- Single-use tokens for email verification and password reset.
CREATE TABLE user_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose     text NOT NULL, -- 'email_verify' | 'password_reset'
    token_hash  bytea NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz
);

CREATE INDEX idx_user_tokens_user_purpose ON user_tokens (user_id, purpose);

-- Identity actions (register, login, password reset, API-token changes) are not
-- workspace-scoped, so the audit trail allows a NULL workspace for them. Actions
-- that DO belong to a workspace still record it (and projects passes it through).
ALTER TABLE audit_events ALTER COLUMN workspace_id DROP NOT NULL;

-- Pending invitations of an email into a workspace at a given role.
CREATE TABLE invitations (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email        text NOT NULL,
    role         text NOT NULL DEFAULT 'member',
    token_hash   bytea NOT NULL UNIQUE,
    invited_by   uuid NOT NULL REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    accepted_at  timestamptz,
    UNIQUE (workspace_id, email)
);

-- +goose Down
-- The Up made workspace_id nullable so user-scoped events (login, register, password reset)
-- could be audited. Remove those workspace-less rows before restoring NOT NULL, otherwise
-- the constraint fails validation against existing data.
DELETE FROM audit_events WHERE workspace_id IS NULL;
ALTER TABLE audit_events ALTER COLUMN workspace_id SET NOT NULL;
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS user_tokens;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS sessions;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified;
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
