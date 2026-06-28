# Getting started (private alpha)

> [!WARNING]
> Plorigo is in **early development** and this is a **private alpha**. Expect rough edges,
> breaking changes, and the occasional need to reset state. **Don't put data you can't lose on
> it yet.** We'd love your help finding the sharp corners — see [Giving feedback](#giving-feedback).

This guide walks you from nothing to a running app on a server you control. It's intentionally
short and links to the deeper docs for each step rather than repeating them.

## What you can do today

The deployment loop works end to end for a focused set of sources:

- **Deploy from a public Git repo** that has a **Dockerfile** — it's built with BuildKit and run.
- **Deploy a public Git repo with no Dockerfile** when it's a recognized framework
  (**Node.js / Vite / Next.js**) — Plorigo detects it and **generates** a Dockerfile.
- **Deploy a prebuilt image**.
- **Add a managed Postgres database** (a one-click template service) and connect your app to it.
- **Custom domains** (via Caddy — HTTP routing today; **automatic SSL is on the roadmap**),
  **build & runtime logs**, a **deploy timeline**, **health checks**, and **one-click rollback** to
  the last healthy version.
- **Environment variables & secrets** per environment (secrets encrypted at rest, write-only).
- **Server health dashboard** and a basic **Production Readiness Doctor** so you can tell whether
  a server and an app are safe to deploy — without SSH.
- **Database backups + restore** for managed Postgres (`pg_dump` to the server's disk).

See the [Roadmap](../ROADMAP.md) for what's coming next.

## Prerequisites

- A **server you control** that runs **Docker** — a cheap VPS, a bare-metal box, anything on
  **Ubuntu 22.04 / 24.04 LTS** with ≥ 1 vCPU / 1 GiB RAM. Port **80** should be reachable for app
  traffic and custom domains; **443** is reserved for the automatic SSL that's on the roadmap.
- The **Plorigo control plane** running somewhere your server can reach. For the alpha you run it
  yourself locally — follow [development.md](./development.md) (Postgres + `make dev` + the
  dashboard). A one-command Docker Compose self-host is on the roadmap.

## The five-minute path

1. **Run the control plane and open the dashboard.** Follow
   [development.md → Running locally](./development.md#running-locally).
2. **Connect a server.** From **Servers → Connect server**, either run the one-line install command
   on your box, or let Plorigo prepare a fresh Ubuntu server over SSH. The server card shows when
   it's **ready to deploy**. Details: [development.md → Connect a server](./development.md#connect-a-server)
   and the [server management model](./architecture/server-management.md).
3. **Create a project and a service.** Point it at a public Git repo (Dockerfile or a detected
   Node/Vite/Next.js app), or a prebuilt image.
4. **Set environment variables.** Add any your app needs on the service's variables page. The
   **Production Readiness Doctor** on the service page flags placeholder values and other gaps.
5. **Deploy.** Watch the **deploy timeline** and **build/runtime logs**. If it fails, you get a
   **plain-English summary** of why (with the raw logs one click away).
6. **Add a domain (optional).** Add a custom hostname and point the DNS record it shows you; once it
   verifies, Plorigo routes traffic to your app. **Domains are HTTP-only for now — automatic SSL is
   on the roadmap.**
7. **Roll back if needed.** Any previous healthy version is one click away — the current release
   keeps serving until the rollback passes its health check.

Need a database? Create a **managed Postgres** service in the same environment, copy its connection
string into your app's variables, and **back it up** from the service's Backups panel.

## Known limitations (alpha)

Being honest about what's *not* there yet:

- **Not production-ready.** APIs, the data model, and behavior are still changing; expect breaking
  changes and the occasional need to reset.
- **Single server.** No multi-server placement, replicas, or autoscaling yet.
- **Sources are limited.** Public Git repos (Dockerfile or detected Node/Vite/Next.js) and prebuilt
  images. Private-repo builds, Docker Compose deploys, and zip upload aren't here yet.
- **Databases.** Managed **Postgres** template only (no Redis/MySQL templates yet); external
  database linking is on the roadmap. A managed database is **ephemeral** — a redeploy starts a
  fresh one (persistent volumes are a later release).
- **Backups** are **local-disk and manual** for now — no off-server (S3) destination or schedules
  yet; restore is a minimal smoke path.
- **No automatic SSL yet.** Custom domains route over **HTTP**; HTTPS / automatic certificates are on
  the roadmap.
- **No preview environments** (per-branch / per-PR) yet, and **no auto-deploy on push** — click
  Redeploy to ship the latest commit.
- **Teams** are basic; advanced roles, approvals, and the AI/MCP gateway are later phases.
- **Self-host only** — there's no managed cloud.

Everything above is tracked on the [Roadmap](../ROADMAP.md).

## Troubleshooting

Hit a snag? See **[troubleshooting.md](./troubleshooting.md)** for the common failures (agent won't
connect, Docker unavailable, build/health-check failures) and where to look.

## Giving feedback

This alpha gets better the more you tell us what broke:

- **Found a bug?** Open a [bug report](https://github.com/Plorigo/plorigo/issues/new/choose) — the
  template asks for the component, steps, your OS, and the agent version.
- **Have an idea or a question?** Start a
  [Discussion](https://github.com/Plorigo/plorigo/discussions).
- **Found a security issue?** Please report it **privately** — see [SECURITY.md](../SECURITY.md).
  Do **not** open a public issue for vulnerabilities.

Good bug reports (what you did, what you expected, what happened, and the relevant logs) are the
single most useful thing you can contribute right now. Thank you. 💜
