-- +goose Up
-- Defense-in-depth for a privileged table: the role is validated in Go, but a DB
-- CHECK guarantees no out-of-vocabulary role can ever be written.
ALTER TABLE workspace_members
    ADD CONSTRAINT workspace_members_role_check
    CHECK (role IN ('owner', 'admin', 'member', 'viewer'));

-- +goose Down
ALTER TABLE workspace_members DROP CONSTRAINT IF EXISTS workspace_members_role_check;
