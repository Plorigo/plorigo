# Troubleshooting

Common problems in the private alpha and where to look. If something here doesn't cover your case,
open a [bug report](https://github.com/Plorigo/plorigo/issues/new/choose) or ask in
[Discussions](https://github.com/Plorigo/plorigo/discussions).

## Where to look first

- **Deploy failures** show a **plain-English summary** on the deployment page, with the **raw
  build and runtime logs** one click away. Read the summary first, then open the logs for the exact
  error.
- **Server problems** show on the **Servers** page and each server's **health detail**: whether the
  agent is online, and whether Docker/Caddy and disk/memory are healthy enough to deploy.
- **App configuration gaps** show in the **Production Readiness Doctor** on the service page
  (missing/placeholder env vars, no running server, domain not verified, no backup, …).

## The agent won't connect / the server shows offline

The agent dials **out** to the control plane and sends a heartbeat; "online/offline" is derived
from the last heartbeat.

- Make sure the server can **reach the control plane URL** you gave the install command (outbound
  HTTPS/HTTP). A control plane on `localhost` is **not** reachable from a remote server.
- The install token is **one-time**. If you didn't copy the command, mint a fresh one from the
  server card (**Re-run install**) — re-running install simply rotates the agent's credential.
- Check the agent's own output (it runs as a systemd service on the box) for the reason it's
  retrying. See [development.md → Connect a server](./development.md#connect-a-server) and the
  [agent model](./architecture/agent.md).

## "Docker is not available on this server"

The agent manages Docker; if the daemon isn't reachable it keeps the server visible but reports
deployments (and backups) as failed.

- Confirm Docker is installed and running on the server (`docker info`).
- The server's health detail shows the reported Docker state. If you connected a **fresh** server,
  let Plorigo prepare it (it installs Docker + Caddy), or install them yourself, then re-run setup.

## A build fails

- A repo with **no Dockerfile** that **isn't** a recognized framework (Node/Vite/Next.js) can't be
  built — add a Dockerfile, or use a supported framework. The failure summary says so.
- For a Dockerfile build, open the **build logs** for the exact step that failed. Builds run on the
  server, so a missing system dependency or a failing build command shows there.
- Only **public** repositories are supported for building right now.

## The app deploys but the health check fails

- A **public** service must accept TCP connections on its container port before traffic is routed.
  If your app listens on a different port, set the **container port** on the service (or add an
  `EXPOSE` to the Dockerfile so it's auto-detected).
- Open the **runtime logs** — the container's own stdout/stderr usually explains a crash on startup.
- The previous healthy release keeps serving, so a failed deploy doesn't take your app down.

## A custom domain stays "pending"

- Add the exact **DNS record** shown on the service's domain panel at your DNS provider, then click
  **Verify**. Once it resolves to your server, the domain goes **active** and serves over **HTTP**
  (automatic SSL is on the roadmap).

## A managed database lost its data after a redeploy

Managed databases are **ephemeral** in the alpha — a redeploy starts a fresh, empty container
(persistent volumes are a later release). **Take a backup** from the service's Backups panel before
redeploying, and **restore** it afterwards from the same panel.

## A backup or restore failed

- Both run on the **database's server agent**. The backup/restore status and the failure reason
  show on the service's Backups panel.
- The database must be **running** to back it up or restore into it. A restore must run on the
  **same server** the backup is stored on (the artifact is on that server's disk).

## Still stuck?

Open a [bug report](https://github.com/Plorigo/plorigo/issues/new/choose) with what you did, what
you expected, what happened, and the relevant logs. For a suspected security issue, report it
**privately** via [SECURITY.md](../SECURITY.md).
