-- +goose Up
-- Extended host-readiness facts (PLO-95), building on 00010_agent_health. The agent reports
-- these on each heartbeat; the control plane derives a richer readiness signal
-- (ready / degraded / blocked / unknown) from them WITHOUT storing the derived state. Like the
-- 00010 columns they are additive and defaulted so existing rows — and agents that predate
-- this slice — read as "not reported": caddy_available is a tri-state (NULL = not yet
-- reported, distinct from a reported false), and cpu_count = 0 marks an agent that does not
-- report the extended facts, so the control plane skips the Caddy/disk/memory checks for it.
ALTER TABLE agents
    ADD COLUMN caddy_available     boolean,                     -- NULL until first reported
    ADD COLUMN caddy_running       boolean NOT NULL DEFAULT false,
    ADD COLUMN caddy_version       text    NOT NULL DEFAULT '',
    ADD COLUMN disk_total_bytes    bigint  NOT NULL DEFAULT 0,
    ADD COLUMN disk_free_bytes     bigint  NOT NULL DEFAULT 0,
    ADD COLUMN mem_total_bytes     bigint  NOT NULL DEFAULT 0,
    ADD COLUMN mem_available_bytes bigint  NOT NULL DEFAULT 0,
    ADD COLUMN cpu_count           integer NOT NULL DEFAULT 0;  -- 0 = extended facts not reported

-- +goose Down
ALTER TABLE agents
    DROP COLUMN caddy_available,
    DROP COLUMN caddy_running,
    DROP COLUMN caddy_version,
    DROP COLUMN disk_total_bytes,
    DROP COLUMN disk_free_bytes,
    DROP COLUMN mem_total_bytes,
    DROP COLUMN mem_available_bytes,
    DROP COLUMN cpu_count;
