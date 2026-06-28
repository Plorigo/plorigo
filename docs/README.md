# Plorigo Documentation

> 🚧 Documentation is being written as the platform takes shape. Expect this section to grow
> quickly. Contributions to the docs are very welcome — see [CONTRIBUTING.md](../CONTRIBUTING.md).

> [!NOTE]
> Contributors & AI agents: start with [AGENTS.md](../AGENTS.md) — it routes you to the right
> design doc for whatever you're working on.

## Start here

- [**Getting started (private alpha)**](./getting-started.md) — from nothing to a running app on a
  server you control: what's supported today, the five-minute path, and the alpha's known limits.
- [Troubleshooting](./troubleshooting.md) — common failures and where to look.

## Architecture & design

The design contract for the platform (intended architecture — see each doc's status note):

- [Overview](./architecture/overview.md) — the four components and how they fit together.
- [Control plane](./architecture/control-plane.md) — the modular monolith and its modules.
- [Server agent](./architecture/agent.md) — the privileged agent and its trust model.
- [Server management](./architecture/server-management.md) — connecting & repairing servers; the SSH bootstrap/management model.
- [Deployment engine](./architecture/deployment-engine.md) — deploy, build, runtime, rollback, proxy.
- [Data & API](./architecture/data-and-api.md) — PostgreSQL data model and ConnectRPC contracts.
- [Jobs & realtime](./architecture/jobs-and-realtime.md) — the job queue and SSE/WebSockets.
- [Dashboard](./architecture/dashboard.md) — the React/TypeScript dashboard.
- [Security model](./architecture/security.md) — secrets, audit, approvals, AI/MCP safety.
- [Principles](./architecture/principles.md) — the invariants behind the rest.
- [Conventions](./conventions.md) — formatting, generated code, commits, testing.

## Building & running

- [Development setup](./development.md) — how to build and run Plorigo locally.

## Planned user guides

The [getting-started guide](./getting-started.md) covers the alpha flow today; these deeper guides
are still being written:

- Self-hosting with Docker Compose.
- Upgrading.
- Environment variables & secrets (reference).

Want a doc that isn't here yet? [Open a documentation issue](https://github.com/Plorigo/plorigo/issues/new/choose)
or start a [Discussion](https://github.com/Plorigo/plorigo/discussions).
