# Backups

The **backups** module (`internal/backups/`) captures a managed Postgres database service's data so
users can trust the platform with production data. The first slice is deliberately small: an
agent-driven `pg_dump` to the server's own disk, tracked end to end.

## Flow

It reuses the deployment job model exactly (see [deployment-engine.md](./deployment-engine.md)):

1. A user (owner/admin/member) calls `CreateBackup(service_id)`. The control plane confirms the
   service is a **managed Postgres** template and is **currently running**, resolves the server its
   container is on (its latest running deployment), and records a `queued` backup row — with an
   audit entry, in one transaction.
2. The **agent on that server** polls `agent.v1.BackupService.PollBackupJob` (a sibling of the
   deploy poll, on the same single agent credential). The control plane atomically claims the next
   queued backup for that server (`FOR UPDATE SKIP LOCKED`) and returns the job plus the database's
   connection credentials.
3. The agent finds the managed Postgres container by its `plorigo.service` label and runs `pg_dump`
   **inside** it (`docker exec`), streaming the SQL dump to a `0700` agent-owned file on the
   server's disk. It computes the artifact's size and SHA-256 while streaming (no buffering), then
   verifies the file is on disk at the expected size.
4. The agent reports each transition (`dumping → verifying → succeeded`, or `failed` with a reason)
   via `ReportBackupJob`. The dashboard shows status and failures per service.

## Credentials & the trust model

The agent does **not** read the container's environment to get the database password. As with a
deployment's secrets, the **control plane** resolves the managed database's `POSTGRES_*` credentials
and sends them in the claimed job — scoped to that one backup (see
[security.md](./security.md)). `PGPASSWORD` is passed to `pg_dump` via the exec environment, never
the argument list, so it never appears in `ps`. The `pg_dump` command is fixed; only the
already-validated user/database identifiers vary, so there is no caller-controlled shell.

The managed database's credentials are plaintext config **variables** today (written at provision
time), so the backup module reads them directly — it needs no master key. The agent never receives
`APP_MASTER_KEY`.

## Destination: local disk first

The MVP destination is the **server's own disk** (`destination = 'local'`). The dump never leaves
the user's machine, so it sits on the same disk as the live database — encrypting it with a key
*also* on that machine would add no real confidentiality. The §8.17 "encrypt before upload"
requirement binds at the **upload boundary**: an **S3-compatible** destination (with a
destination-scoped data key minted per job by the control plane — never the master key) is a later
slice. The data model (`backups.destination` / `artifact_uri`) already accommodates it.

## What's deferred

- S3-compatible remote destination + encrypt-before-upload.
- Scheduled/automatic backups and retention policy.
- Restore (PLO-24 adds a restore job + a `make e2e-backup` controlled test path).
- A live-VPS sign-off (the `pg_dump`/restore path is exercised against real Docker locally; a
  fresh-VPS run is tracked like the other infra sign-offs).
