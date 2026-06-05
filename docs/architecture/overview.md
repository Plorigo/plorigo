# Architecture overview

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

This is the map. Read it first, then jump to the deeper doc for the part you're changing.

## The four components

Plorigo is built from four programs that share one set of API contracts:

| Component | Language | Runs where | Role |
|---|---|---|---|
| **Control plane** | Go | Your dashboard host (or our cloud, later) | API server, web serving, auth, orchestration, background workers |
| **Server agent** | Go | Each connected server | Executes signed jobs: manages Docker, Caddy, builds, backups, health |
| **CLI** | Go | Developer machines / CI | Scriptable access to the control-plane API |
| **Dashboard** | React + TypeScript + Vite | Browser (served by the control plane) | The product UI |

The guiding decision is **simple core, strong guardrails, beautiful UX**: lean on proven
infrastructure pieces (Docker, Caddy, PostgreSQL) rather than inventing a platform from
scratch, and put the polish into the dashboard and the safety workflows.

## How they communicate

```text
  Browser (dashboard) ─┐
                       ├─ ConnectRPC ──▶ Control plane ──┐
  CLI ─────────────────┘                  (PostgreSQL)   │  outbound, signed jobs
                                                          ▼
                                                    Server agent ──▶ Docker / Caddy
```

- **Dashboard & CLI → control plane:** typed [ConnectRPC](./data-and-api.md) calls; realtime
  updates over SSE (deploy/log streams) and WebSockets (terminal) — see
  [jobs-and-realtime.md](./jobs-and-realtime.md).
- **Agent → control plane:** the agent opens an **outbound** secure connection and pulls
  **signed, scoped** jobs. The control plane does not need inbound SSH to the server. See
  [agent.md](./agent.md) for the trust model.

## Repository layout

The intended layout (see also the [README](../../README.md#repository-structure)):

```text
plorigo/
├── apps/
│   └── web/            # React + Vite dashboard
├── cmd/
│   ├── controlplane/   # Go API + workers + web serving
│   ├── agent/          # Go server agent (runs on your servers)
│   └── cli/            # Go CLI
├── internal/           # control-plane modules (auth, deployments, secrets, backups, …)
├── proto/              # ConnectRPC / protobuf contracts
├── migrations/         # SQL migrations
├── deploy/             # docker-compose, Caddy, systemd
├── docs/               # documentation
└── scripts/            # dev & ops scripts
```

The control plane is a **modular monolith** — one binary, internal modules. The module list
and self-host shape live in [control-plane.md](./control-plane.md).

## The deploy flow, at a glance

A deployment is a record plus a job the agent executes. The happy path:

1. Trigger (Git event or manual) → control plane creates a deployment record and a job.
2. Agent receives the signed job and fetches the source.
3. Build detection runs; an image is built (BuildKit, Nixpacks fallback).
4. A new container starts on an isolated Docker network; a health check runs.
5. Caddy switches the route to the new version; the previous version is kept for rollback.
6. Logs and metrics keep streaming.

Full detail — build priority, rollback, runtime decisions — is in
[deployment-engine.md](./deployment-engine.md).

## Where to go next

- [control-plane.md](./control-plane.md) — modular-monolith internals and self-host shape
- [agent.md](./agent.md) — the privileged server agent and its trust model
- [deployment-engine.md](./deployment-engine.md) — deploy, build, runtime, rollback, proxy
- [data-and-api.md](./data-and-api.md) — PostgreSQL data model + ConnectRPC contracts
- [jobs-and-realtime.md](./jobs-and-realtime.md) — background jobs + SSE/WebSockets
- [dashboard.md](./dashboard.md) — the React dashboard
- [auth.md](./auth.md) — identity, sessions, API tokens, and the authorization seam
- [security.md](./security.md) — security model, secrets, AI/MCP safety
- [principles.md](./principles.md) — the engineering principles behind all of the above
