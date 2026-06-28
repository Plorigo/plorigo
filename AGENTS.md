# AGENTS.md — guide for AI agents & contributors

Plorigo is an open-source **BYOS (Bring Your Own Server) deployment platform**: a Go
control plane, a Go server agent, a Go CLI, and a React/TypeScript dashboard. See
[README.md](./README.md) for the product overview and [ROADMAP.md](./ROADMAP.md) for
what's planned and what's free vs. paid.

This file is the entry point for anyone (human or AI) writing code here. It is intentionally
short: it tells you the rules that always apply and **points you to the deeper doc to read
before you touch a given subsystem**. Read the routed doc first — don't reinvent a design
that's already written down.

## Status & framing

> [!WARNING]
> Plorigo is in **early development**. Most of the code described in the docs does not exist
> yet. The documents under [`docs/architecture/`](./docs/architecture/) describe the
> **target architecture — a design contract**, not shipped functionality. Write code that
> matches them, and when the design genuinely needs to change, update the relevant doc **in
> the same pull request**.

## Source boundary (please read)

This repository is **public**. Everything you add to it — code, comments, and docs — is
public too. When writing or expanding documentation:

- Document **engineering design and decisions** only.
- Do **not** add product feature roadmaps, pricing, business strategy, internal metrics, or
  competitor analysis. For anything roadmap-ish, **link [ROADMAP.md](./ROADMAP.md)** instead
  of enumerating planned features.
- **Never commit secrets.** Secret scanning and push protection are enabled on this repo.

## Always-on rules

- **Match existing conventions** — see [docs/conventions.md](./docs/conventions.md) and
  [CONTRIBUTING.md](./CONTRIBUTING.md).
- **Generated code is never hand-edited.** Protobuf is generated with `buf`, SQL with `sqlc`;
  change the source (`.proto` / `.sql`) and regenerate.
- **Privileged paths are high-risk.** Anything touching the agent, Docker, Caddy, secrets,
  backups, the terminal, or the AI/MCP gateway gets extra scrutiny and needs testing against a
  real Docker environment. Never broaden what an AI agent or an unprivileged user can do
  without first reading [docs/architecture/security.md](./docs/architecture/security.md).
- **Follow the engineering principles** in
  [docs/architecture/principles.md](./docs/architecture/principles.md): every scary action has
  a recovery path; preview before production; plain English first, raw details always available.
- Use [Conventional Commits](https://www.conventionalcommits.org/); PRs are squash-merged.

## Documentation map — read the right doc before you work

| When you're working on… | Read first |
|---|---|
| Anything (first orientation) | [docs/architecture/overview.md](./docs/architecture/overview.md) |
| A control-plane `internal/*` module, the modular-monolith structure, or the self-host shape | [docs/architecture/control-plane.md](./docs/architecture/control-plane.md) |
| **Adding a new control-plane module** (file pattern, consumer-defined ports, boundary rules) | [docs/architecture/modules.md](./docs/architecture/modules.md) |
| The server **agent** (registration, signed jobs, Docker/Caddy management, policy checks) | [docs/architecture/agent.md](./docs/architecture/agent.md) |
| **Connecting/preparing a server** (dashboard-managed setup, the SSH bootstrap/management channel, its security model) | [docs/architecture/server-management.md](./docs/architecture/server-management.md) |
| The **deploy / build / rollback** flow, build detection, container runtime, or Caddy routing | [docs/architecture/deployment-engine.md](./docs/architecture/deployment-engine.md) |
| The **database** (pgx/sqlc/goose, schema, migrations) or the **API** (proto/ConnectRPC/buf) | [docs/architecture/data-and-api.md](./docs/architecture/data-and-api.md) |
| **Background jobs** (Postgres queue) or **realtime** (SSE / WebSockets) | [docs/architecture/jobs-and-realtime.md](./docs/architecture/jobs-and-realtime.md) |
| The **dashboard** (React/Vite/TanStack/Tailwind/shadcn, realtime UI) | [docs/architecture/dashboard.md](./docs/architecture/dashboard.md) |
| **Auth** (login/registration, sessions, API tokens, RBAC) or the **authorization (policy) seam** | [docs/architecture/auth.md](./docs/architecture/auth.md) |
| **Secrets, audit, permissions/approvals, or the AI/MCP gateway** | [docs/architecture/security.md](./docs/architecture/security.md) |
| **Database backups** (agent-driven `pg_dump`, the backup job model, destinations) | [docs/architecture/backups.md](./docs/architecture/backups.md) |
| Coding style, commits, testing, or generated code | [docs/conventions.md](./docs/conventions.md) |
| Reporting a vulnerability (process, not design) | [SECURITY.md](./SECURITY.md) |

## Building & running

See [docs/development.md](./docs/development.md). Toolchain: **Go**, **Node + pnpm**,
**Docker**, `buf`, `sqlc`, `goose`, `golangci-lint`, and **Caddy**.
