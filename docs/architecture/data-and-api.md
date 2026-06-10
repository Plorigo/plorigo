# Data model & API contracts

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

Two kinds of contract live here: the **database schema** (source of truth) and the **API
protos** (how everything talks). Read this before adding a table, writing a migration, or
changing a `.proto`.

## Database: PostgreSQL

PostgreSQL is the single source of truth. The platform needs relational integrity,
transactions, and an audit history, so we use a relational database — not a document store.

Tooling:

- **`pgx`** — the Go PostgreSQL driver.
- **`sqlc`** — type-safe Go from hand-written SQL. **Generated code is never hand-edited**;
  change the `.sql` and regenerate.
- **`goose`** (or Atlas) — migrations.

### Core entities

The data model is organized around how users think — workspace → project → environment →
deployment, plus the servers and resources behind them. Intended core tables include:

```text
users                workspaces           workspace_members
projects             environments         services
servers              agents               agent_jobs
deployments          deployment_steps     domains
source_connections   project_sources
env_vars             secrets              resources
backups              restore_jobs         audit_events
invitations          log_streams          readiness_checks
approval_requests    ai_agent_sessions
sessions             api_tokens           user_tokens
agent_registration_tokens
```

Token tables (`sessions`, `api_tokens`, `user_tokens`, `agent_registration_tokens`) and the
agent's `credential_hash` store only **hashed** tokens, never the raw value — see
[auth.md](./auth.md) and [agent.md](./agent.md). The append-only `audit_events` allows a
NULL `workspace_id` for user-scoped actions (login, password reset).

Treat these as **schema**, not feature promises — a table existing does not mean the feature
on top of it is built or committed. For what's actually planned, see [ROADMAP.md](../../ROADMAP.md).

### Migration conventions

- Migrations are **forward-only** and reviewed like code.
- After a schema change, **regenerate `sqlc`** in the same change; never hand-edit generated files.
- Privileged data — secret values, and the GitHub OAuth tokens in `source_connections` —
  follows the rules in [security.md](./security.md): store ciphertext and metadata, never
  plaintext.

## API: ConnectRPC + Protocol Buffers

APIs use **Protocol Buffers** with **ConnectRPC**. Typed contracts give the backend, agent,
CLI, dashboard, and the future MCP layer one shared definition and generated clients, instead
of many hand-written REST clients.

### Proto layout

```text
proto/
  controlplane/v1/    # auth, workspaces, projects, environments, deployments, secrets, …
  agent/v1/           # control plane ↔ agent
  mcp/v1/             # AI/MCP tools (see security.md)
```

### Conventions

- Code is generated with **`buf`**; **never hand-edit** generated `*.pb.go` / `*_connect.go`.
  Change the `.proto` and run `buf generate`.
- Packages are **versioned** (`v1`, `v2`, …); evolve compatibly within a version.
- The dashboard and CLI consume the **generated typed clients** — see [dashboard.md](./dashboard.md).
- A REST wrapper over these contracts can be added **later if needed** for public-API users;
  it is not a current deliverable.
