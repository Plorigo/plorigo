# Background jobs & realtime

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

Most real work in Plorigo is asynchronous (builds, backups) and most of the UI is live (deploy
timelines, streaming logs, terminals). This doc covers both. The deploy/agent/dashboard docs
point here instead of restating the queue and the streaming model.

## Background jobs

**Decision:** start with a **Postgres-backed job queue** (a River-style queue), reusing the
database we already run. This keeps the self-host footprint to one database and one binary.

Representative kinds of work that run as jobs (mechanism, not a feature list):

- build an image, deploy an app, run a health check
- configure a domain, issue SSL
- back up / restore a database or volume
- send a notification, clean up expired previews

Jobs are durable, retryable, and observable — a deployment record links to the job that
fulfills it (see [deployment-engine.md](./deployment-engine.md)). Agent-executed jobs are
**signed and scoped**; see [agent.md](./agent.md).

> [!NOTE]
> **Temporal** is an explicit *later-if-needed* option, only if deployment workflows outgrow a
> simple state machine / queue. Don't add it on day one.

> [!NOTE]
> The durable queue is **not built yet**. The first async control-plane work —
> dashboard-managed server setup (`internal/serversetup`, see
> [server-management.md](./server-management.md)) — runs as an **in-process goroutine** that
> persists ordered steps to `server_setup_runs` / `server_setup_events`, which the dashboard
> polls (the same persist-and-poll model as deployment events). It is bounded by a timeout and
> records a terminal status even on cancel; a process restart mid-run leaves the run for the
> user to retry. Migrating these onto the queue is a follow-up.

## Realtime

Two transports, chosen per use case:

| Transport | Used for | Why |
|---|---|---|
| **SSE** (Server-Sent Events) | Deploy status and log streams | One-way, simple, reconnect-friendly, plays well with proxies |
| **WebSockets** | The web terminal | Bidirectional, interactive |

The terminal is powerful and dangerous — it is permission-gated, audited, carries a production
warning, and is **disabled for AI agents**. Those rules live in [security.md](./security.md).
Persistent/reconnectable terminal sessions are a later enhancement, not part of the initial design.
