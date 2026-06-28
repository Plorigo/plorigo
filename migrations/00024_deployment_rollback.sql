-- +goose Up
-- One-click rollback records the action in deployment history: a rollback enqueues a new
-- deployment that reproduces an earlier healthy deployment's artifact, and rolled_back_from
-- links the new row to the deployment it restores. Nullable + self-referential; a normal
-- deploy leaves it NULL. ON DELETE SET NULL so deleting an old deployment never blocks or
-- cascades into the rollback that referenced it.
ALTER TABLE deployments
    ADD COLUMN rolled_back_from uuid REFERENCES deployments (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE deployments DROP COLUMN rolled_back_from;
