-- +goose Up
-- Give each backup a human-facing identity. Until now a backup row carried only a UUID, size, and
-- timestamp, so a list of them was indistinguishable. Two columns fix that:
--   label          an optional name the operator types when taking a backup ("before v2 migration").
--   trigger_source who started it. Dashboard-initiated backups are 'manual'; scheduled backups are
--                  a later slice (see docs/architecture/backups.md) and will record 'scheduled'.
-- Both default so existing rows and the agent's claim/report path are unaffected. The CHECK on
-- trigger_source mirrors the Go vocabulary (backups.Trigger*), the same defense-in-depth as status.
ALTER TABLE backups
    ADD COLUMN label          text NOT NULL DEFAULT '',
    ADD COLUMN trigger_source text NOT NULL DEFAULT 'manual'
        CHECK (trigger_source IN ('manual', 'scheduled'));

-- +goose Down
ALTER TABLE backups
    DROP COLUMN trigger_source,
    DROP COLUMN label;
