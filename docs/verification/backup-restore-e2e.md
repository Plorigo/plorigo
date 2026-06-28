# Verification: database backup → restore (PLO-23, PLO-24)

> [!IMPORTANT]
> A backup is only valuable if **restore works**. This runbook proves the full round-trip against
> **real Docker**: provision a managed Postgres service, seed rows, back it up with `pg_dump`,
> destroy the data, restore the backup with `psql`, and assert the rows came back. Record the
> result (commands, date, outcome) in the PR that claims it, using the template at the bottom.

See [backups.md](../architecture/backups.md) for the design.

## Verification levels (where this fits)

| Level | What | Where it runs |
|---|---|---|
| Hermetic | The backups/restore service state machine — authorization, the running-database precondition, claim + credential resolution, agent ownership of reports | **CI** — `make test` (`internal/backups/service_test.go`, fakes, no DB) |
| **Real Docker (this doc)** | **Provision → seed → `pg_dump` → drop → `psql` restore → assert rows** | `make e2e-backup` (local, **not CI**) |
| Real VPS / S3 | Backup survives on a server you don't control; off-server (S3) destination | **deferred** — tracked like the other infra sign-offs |

The driver is `internal/app/e2e_backup_test.go` (`//go:build e2e`), run by `make e2e-backup`. This
doc is the authoritative procedure and the place to record the run.

## Prerequisites

- **Docker** with the `docker` CLI on `PATH` (the agent runs `pg_dump`/`psql` inside the managed
  Postgres container; the test seeds and asserts via `docker exec`).
- **Caddy** on `PATH` (the agent requires its router to be configured to run any deployment, even
  a private database that gets no route).
- A **migrated Postgres** for the control plane, and `DATABASE_URL` + `APP_MASTER_KEY` set — the
  same prerequisites as `make e2e-build` (see [development.md](../development.md)).
- Network access to pull `postgres:16-alpine`.

## Procedure

```bash
# Postgres up + migrated, with the control plane env vars set (see development.md):
export DATABASE_URL=postgres://plorigo:plorigo@localhost:5432/plorigo?sslmode=disable
export APP_MASTER_KEY="$(openssl rand -base64 32)"

make e2e-backup
```

`make e2e-backup` builds a native agent binary and runs `TestE2EBackupRestore`. The test:

1. Boots an in-process control plane and starts the **real agent** as a host subprocess.
2. Provisions a managed Postgres service (`CreateDatabase`, deploy-now) and waits for it to run.
3. Seeds `widgets` with three rows via `docker exec ... psql`.
4. `CreateBackup` → waits for the backup to reach **succeeded** (the agent runs `pg_dump` inside
   the container, writes the dump to its data dir, records size + sha256).
5. `DROP TABLE widgets` to destroy the data.
6. `RestoreBackup` → waits for the restore to reach **succeeded** (the agent pipes the dump into
   `psql` inside the container).
7. Asserts `widgets` is back with three rows.

It **skips** (does not fail) when `DATABASE_URL`/`APP_MASTER_KEY` are unset or `docker`/`caddy` are
missing — so it never runs in CI by accident.

## What this does NOT cover (deferred)

- An **off-server S3-compatible destination** with encrypt-before-upload — the MVP writes to the
  server's own disk. The data model is ready for it.
- **Scheduled** backups and retention.
- A **fresh-VPS** run (the `pg_dump`/`psql` path runs against real Docker here, on the same host;
  a real remote VPS sign-off is tracked like the other infra gates).

## Record of runs

| Date | Commit | Docker / Postgres version | Outcome | Notes |
|---|---|---|---|---|
| _yyyy-mm-dd_ | _sha_ | _e.g. Docker 27 / pg16_ | _pass/fail_ | _backup bytes, anything surprising_ |
