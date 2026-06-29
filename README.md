<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/brand/plorigo-logo-white.png">
  <img alt="Plorigo" src="assets/brand/plorigo-logo-black.png" width="340">
</picture>

### Launch with control.

Deploy apps to servers **you** control — a Vercel-like deployment platform for developers,
teams, agencies, and AI-built apps. Get previews, domains, SSL, logs, databases, backups,
rollbacks, and production safety — on infrastructure you own, without surprise platform bills.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPLv3-blue.svg)](./LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](./CONTRIBUTING.md)
[![Made with Go](https://img.shields.io/badge/Go-control%20plane%20%7C%20agent%20%7C%20cli-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Made with React](https://img.shields.io/badge/React%20%2B%20TypeScript-dashboard-61DAFB?logo=react&logoColor=white)](https://react.dev)

[Website](https://plorigo.com) · [Roadmap](./ROADMAP.md) · [Contributing](./CONTRIBUTING.md) · [Security](./SECURITY.md) · [Discussions](https://github.com/Plorigo/plorigo/discussions) · [Sponsor](#-support-plorigo)

</div>

---

> [!WARNING]
> **Plorigo is in early development.** The architecture, APIs, and data model are still
> changing and it is **not yet ready for production use**. We are building in the open —
> star the repo and watch the [roadmap](./ROADMAP.md) to follow along, and jump into
> [Discussions](https://github.com/Plorigo/plorigo/discussions) to help shape it.

## What is Plorigo?

Plorigo is an **open-source BYOS (Bring Your Own Server) deployment platform**. You connect
a server you control — a cheap VPS, a bare-metal box, anything that runs Docker — and Plorigo
gives you the deployment experience you expect from Vercel or Railway: Git deploys, preview
URLs, automatic SSL, logs, databases, backups, and one-click rollbacks.

Two things make it different:

1. **You own the infrastructure.** No metered traffic, no surprise platform bills — you pay
   your server provider, and that's it.
2. **Production safety is built in — including for AI-built apps.** Plorigo checks secrets,
   databases, domains, and backups before you go live, explains failures in plain English,
   and keeps AI agents on a tight leash.

> Your AI can read logs and suggest fixes. It cannot delete production data.

## Who it's for

- **Developers & indie hackers** — deploy side projects and SaaS apps on affordable VPS hosts (Hetzner, DigitalOcean, Scaleway, …) with modern DX.
- **Coolify / Dokploy switchers** — a cleaner dashboard with safer production workflows, better backups, and easier team isolation.
- **Vercel / Railway switchers** — keep Git deploys and previews; lose the billing anxiety and runtime limits.
- **Agencies & freelancers** — host many client apps on servers you control, with isolated workspaces and safe client access.
- **AI-assisted / "vibe" builders** — take an app built with Cursor, Lovable, Bolt, Replit, Claude Code, Windsurf, or v0 and launch it safely.
- **Internal tool builders** — let teams ship with AI, but deploy with guardrails.

## Highlights (planned & in progress)

See the [**Roadmap**](./ROADMAP.md) for what's shipping when. The platform is being built around:

- 🚀 **Deployment engine** — deploy from GitHub, Dockerfile, or Docker Compose, with build/runtime logs, health checks, and one-click rollback.
- 🌐 **Domains & SSL** — custom domains with automatic Let's Encrypt SSL via Caddy.
- 🔑 **Secrets done right** — per-environment, encrypted at rest, redacted in logs, write-only where possible.
- 🗄️ **Databases & backups** — Postgres/Redis services, scheduled backups to S3-compatible storage, and *restore confidence*.
- 👀 **Preview environments** — per-branch / per-PR previews you can share.
- 🩺 **Production Readiness Doctor** — catches missing env vars, hardcoded secrets, and unsafe defaults before launch.
- 🤖 **Safe AI-agent gateway (MCP)** — read logs and create previews; never delete production data.
- 👥 **Teams & agencies** — workspaces, roles, audit logs, and isolated client access.

## Tech stack

| Area | Choice |
|---|---|
| Control plane · agent · CLI | **Go** |
| Dashboard | **React + TypeScript + Vite**, TanStack Router/Query, Tailwind + shadcn/ui |
| API | **ConnectRPC / Protocol Buffers** |
| Database | **PostgreSQL** (pgx + sqlc), goose migrations |
| Jobs | Postgres-backed queue |
| Runtime / build | **Docker Engine** + BuildKit (Nixpacks fallback) |
| Reverse proxy / SSL | **Caddy** |
| Object storage | S3-compatible (MinIO, R2, S3, B2, Hetzner) |
| Self-host | **Docker Compose** |

## Repository structure

```text
plorigo/
├── apps/
│   └── web/            # React + Vite dashboard
├── cmd/
│   ├── controlplane/   # Go API + workers + web serving
│   ├── agent/          # Go server agent (runs on your servers)
│   └── cli/            # Go CLI
├── internal/           # auth, deployments, servers, secrets, backups, …
├── proto/              # ConnectRPC / protobuf contracts
├── migrations/         # SQL migrations
├── deploy/             # docker-compose, Caddy, systemd
├── docs/               # documentation
└── scripts/            # dev & ops scripts
```

## Getting started

Plorigo is in a **private alpha** — the deployment loop works end to end for a focused set of
sources. The [**Getting started guide**](./docs/getting-started.md) takes you from nothing to a
running app on a server you control, and is honest about the alpha's limits; if something breaks,
the [troubleshooting guide](./docs/troubleshooting.md) is the place to look.

> 🚧 A one-command Docker Compose self-host is on the way. For now you run the control plane
> yourself — see [development.md](./docs/development.md). Watch the [roadmap](./ROADMAP.md), and
> see [CONTRIBUTING.md](./CONTRIBUTING.md) to help build it.

## License & open core

Plorigo's core is licensed under the **GNU Affero General Public License v3.0** ([LICENSE](./LICENSE)).

In plain English:

- ✅ You can **self-host Plorigo for free**, forever, on your own infrastructure.
- ✅ You can **modify it** and use it however you like internally.
- ⚖️ If you **run a modified version as a network service** for others, AGPL requires you to
  **publish your source** under the same license. This keeps the project open and prevents
  closed-source clones.
- 🏢 A separate **commercial license** is available for organizations that need to embed
  Plorigo in a proprietary product or want terms other than AGPL — this is part of how the
  project sustains itself (alongside a future managed cloud).

The **Plorigo** name and logo are trademarks and are **not** covered by the AGPL — see
[TRADEMARK.md](./TRADEMARK.md). You're welcome to run and fork the code; please don't pass off
a fork as the official Plorigo product.

## 💜 Support Plorigo

Plorigo is independent and open source (AGPL-3.0). Sponsorships fund focused work on the
**free, self-hostable core** — the deployment engine, agent safety, backups, and docs everyone
keeps forever.

[![Sponsor on GitHub](https://img.shields.io/badge/Sponsor-GitHub_Sponsors-EA4AAA?logo=githubsponsors&logoColor=white)](https://github.com/sponsors/Plorigo)
[![Fund on Polar](https://img.shields.io/badge/Fund-Polar-0062FF)](https://polar.sh/plorigo)

- **Individuals** — a recurring sponsorship of any size genuinely helps and keeps you in the loop.
- **Companies** — sponsor a tier to get your logo on this README and back the infrastructure tooling
  your team relies on. Invoices/VAT are handled (Polar is a merchant of record), so it's easy to
  expense. Details in **[SPONSORS.md](./SPONSORS.md)**.

Not able to sponsor? **⭐ Star the repo**, file good bug reports, improve the docs, or tell a
friend — that support matters just as much.

## Community & contributing

- 💬 **Questions & ideas** → [GitHub Discussions](https://github.com/Plorigo/plorigo/discussions)
- 🐛 **Bugs** → [open an issue](https://github.com/Plorigo/plorigo/issues/new/choose)
- 🔐 **Security** → please report privately, see [SECURITY.md](./SECURITY.md)
- 🤝 **Contributing** → start with [CONTRIBUTING.md](./CONTRIBUTING.md) and our [Code of Conduct](./CODE_OF_CONDUCT.md)

We welcome contributions of all sizes. Because Plorigo handles servers, secrets, and
production data, every pull request is reviewed for correctness and safety — see the
contribution guide for what that looks like.
