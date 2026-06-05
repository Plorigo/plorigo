# Control plane

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

The control plane is the brain: it serves the API and dashboard, holds state, and coordinates
agents. Read this before working on anything under `internal/` or `cmd/controlplane/`.

## Decision: a modular monolith

Start as a **modular monolith**, not microservices. One Go service provides the API server,
static dashboard serving, auth/sessions, background workers, the agent gateway, and the
SSE/WebSocket endpoints. Modules talk in-process through clear interfaces; we can split a
module into its own service later if a real scaling need appears — but not before.

Why: a small team ships and operates one binary far more easily than a fleet of services, and
self-hosters get the fewest moving parts.

## Internal modules

Each module owns one slice of the domain and exposes a small interface to the rest. Intended
modules under `internal/`:

| Module | Owns |
|---|---|
| `auth` | Users, sessions, API tokens, email verification & password reset — see [auth.md](./auth.md) |
| `audit` | The append-only audit trail of sensitive actions |
| `projects` | Projects and the workspace aggregate: workspaces, membership/roles, and invitations (**writes**) |
| `membership` | Read-only role lookup over workspace membership — the port `policy` consumes (provider-only, like `audit`) |
| `environments` | Environments (preview / staging / production) |
| `deployments` | Deployment records and orchestration — see [deployment-engine.md](./deployment-engine.md) |
| `servers` | Connected servers and their metadata |
| `agents` | Agent registration, keys, and the job gateway — see [agent.md](./agent.md) |
| `builders` | Build detection and image builds |
| `docker` | Container/network/volume operations (executed by the agent) |
| `caddy` | Reverse-proxy / SSL desired state |
| `domains` | Custom domains and verification |
| `secrets` | Encrypted secret storage and scoping — see [security.md](./security.md) |
| `backups` | Backup and restore jobs |
| `logs` | Build/runtime log capture and streaming |
| `metrics` | Server and container health metrics |
| `terminal` | Permission-gated web terminal |
| `policy` | Authorization and the guardrails enforced before risky actions |
| `ai` | The AI production-safety layer (analysis, plain-English summaries) |
| `mcp` | The AI/MCP gateway — tiered, audited tools (see [security.md](./security.md)) |

> [!NOTE]
> Treat `ai` and `mcp` as **safety-critical**. What those modules are allowed to do is defined
> by the policy model in [security.md](./security.md), not by individual feature work. Don't
> expand their capabilities without going through that doc.

## Persistence, jobs, realtime

- **State** lives in PostgreSQL — the single source of truth. Data model and access patterns:
  [data-and-api.md](./data-and-api.md).
- **Work** runs through a Postgres-backed job queue, and **realtime** uses SSE + WebSockets:
  [jobs-and-realtime.md](./jobs-and-realtime.md).
- **Contracts** between the control plane, agent, CLI, and dashboard are ConnectRPC/protobuf:
  [data-and-api.md](./data-and-api.md).

## Self-host shape

The control plane is designed to run with as few parts as possible — `docker compose up -d`:

```text
controlplane   # the Go binary (API + workers + dashboard)
postgres       # state
minio          # optional, for S3-compatible backup storage in local/self-host setups
```

Self-hosting is free and first-class — see the open-core section of
[ROADMAP.md](../../ROADMAP.md) for how the free core and any managed offering relate.
