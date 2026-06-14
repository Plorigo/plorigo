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
service → deployment, plus the servers and resources behind them. Intended core tables include:

```text
users                workspaces           workspace_members
projects             environments         services
servers              agents               agent_jobs
deployments          deployment_steps     domains
source_connections
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

### Services

A **service** is one deployable component of a project — its own source, port, URL, env vars,
and deployment history. A project is a *system* made of one or more services (`web`, `api`,
`worker`, `db`), each with a **different source**; a service lives in exactly **one**
environment, so the same app in production and preview is two services. The `services` row
**denormalizes `project_id` and `workspace_id`** from its environment (both immutable), so
authorization, scoping, and the project/workspace views need no joins — mirroring
`deployments`. `slug` is `UNIQUE (environment_id, slug)` and doubles as the service's DNS
alias on its [per-environment Docker network](./deployment-engine.md).

The Git/image **source is folded onto the service row** (this replaces the former
`project_sources` table), discriminated by a `source_kind` (`image` | `git` | `template`),
mirroring how `deployments` folds its image/git columns. A `git` service stores the same
repo fields as before — `connection_id` (NULL for a public repo), `provider`, `owner`,
`repo`, `branch`, `html_url`, and an `access` discriminator (`oauth` | `public` | `app`,
with the same access↔connection invariant) — alongside `container_port` and a `visibility`
(`public` | `private`). A **public** service carries a `route_url` (kept current from the
latest running deployment); a **private** one has none (see
[deployment-engine.md](./deployment-engine.md)). A `deployments` row carries a
**`service_id`** (its owning service); `environment_id` / `project_id` / `workspace_id` stay
denormalized alongside it.

Custom domains are service-scoped in the `domains` table. A service can have multiple custom
hostnames, while each hostname is unique within a workspace so routing cannot be ambiguous.
The generated `route_url` stays the baseline address and DNS target. Domain rows track
plain-English state (`blocked`, `pending_dns`, `verified`, `active`, `failed`) plus the last
verification time; automatic HTTPS is a later slice.

### Migration conventions

- Migrations are **forward-only** and reviewed like code.
- After a schema change, **regenerate `sqlc`** in the same change; never hand-edit generated files.
- Privileged data — secret values, and the GitHub OAuth tokens in `source_connections` —
  follows the rules in [security.md](./security.md): store ciphertext and metadata, never
  plaintext. A `git` **service** records how the repo is reached in an `access` column
  (`oauth` | `public` | `app`); a **public** source has a **NULL `connection_id`** and stores no
  credential at all, so `connection_id` is nullable and the reads `LEFT JOIN source_connections`.
- **`env_vars` are service-scoped** (`env_vars.service_id`), since each service is its own app.
  **`secrets` stay environment-scoped** — a deliberate asymmetry this round; a follow-up may
  align them (see [security.md](./security.md)).
- A `deployments` row records a **`source_kind`** (`image` | `git`). A `git` deployment also
  stores `clone_url`, `git_ref`, `source_access` (`public` only for now), and the
  agent-reported `commit_sha` / `built_image_ref`; `image_ref` is empty until/unless built. The
  dashboard triggers a redeploy with `DeploymentService.CreateDeploymentForService`, which
  resolves the service's source **server-side** (the request carries no repo URL) — see
  [deployment-engine.md](./deployment-engine.md).

## API: ConnectRPC + Protocol Buffers

APIs use **Protocol Buffers** with **ConnectRPC**. Typed contracts give the backend, agent,
CLI, dashboard, and the future MCP layer one shared definition and generated clients, instead
of many hand-written REST clients.

### Proto layout

```text
proto/
  controlplane/v1/    # auth, workspaces, projects, environments, services, deployments, secrets, …
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

### Service surface (where the new entity lands)

Inserting **Service** between environment and deployment reshapes a few services:

- **`ServiceService`** (new) is the entry point: `CreateService` (with `deploy_now` →
  `{service, deployment_id}`), `GetService`, `ListServicesBy{Environment,Project,Workspace}`,
  `UpdateServiceSource`, `UpdateServiceVisibility`, `DeleteService`.
- **`DeploymentService`** drops `CreateDeployment` / `CreateDeploymentFromSource` and gains
  `CreateDeploymentForService` (redeploy a service) and `ListDeploymentsByService`;
  `Deployment` carries a `service_id`.
- **`EnvVarService`** is rekeyed from `environment_id` to `service_id`.
- **`SourceService`** shrinks to discovery + connection — `GetConnection`,
  `ListRepositories`, `ListBranches`, `DisconnectGitHub`. Connecting a repo is now part of
  creating a service, so the old connect / `GetProjectSource` / `ListSourcesByWorkspace` /
  disconnect RPCs and the `Source` message are gone.
