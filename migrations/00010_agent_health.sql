-- +goose Up
-- Server health & Docker compatibility (see docs/architecture/agent.md). The agent
-- reports these facts on each heartbeat; the control plane derives a readiness signal
-- (ready / degraded / unavailable) from them WITHOUT storing the state (like liveness).
-- The columns are additive and nullable/defaulted so existing rows — and agents that
-- predate health reporting — read as "unknown" until the next beat. docker_available is
-- a tri-state: NULL means "not yet reported", distinct from a reported false.
ALTER TABLE agents
    ADD COLUMN docker_available boolean,                  -- NULL until the first health report
    ADD COLUMN docker_version   text NOT NULL DEFAULT '', -- Docker daemon version; '' when unknown
    ADD COLUMN os               text NOT NULL DEFAULT '', -- host GOOS; non-empty marks a health-reporting agent
    ADD COLUMN arch             text NOT NULL DEFAULT ''; -- host GOARCH

-- +goose Down
ALTER TABLE agents
    DROP COLUMN docker_available,
    DROP COLUMN docker_version,
    DROP COLUMN os,
    DROP COLUMN arch;
