# Server agent

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

The agent is the most security-sensitive part of Plorigo: a privileged program running on the
user's server that can touch containers, secrets, backups, and the proxy. Read this — and
[security.md](./security.md) — before changing anything under `cmd/agent/` or `internal/agents`.

## What it is

A small **Go binary** installed on each connected server, typically run as a **systemd
service**. It is designed to:

- Register with the control plane using a **one-time token**.
- Open an **outbound** secure connection to the control plane (no inbound SSH required).
- Receive **signed, scoped** jobs, validate them, and execute them.
- Manage **Docker** (containers, networks, volumes) and **Caddy** (routes, SSL).
- Build images with **BuildKit**.
- Stream logs, run health checks, and run backups/restores.
- Report CPU / RAM / disk / container health back to the control plane.
- Apply **policy checks** before any risky action.

## Trust model

The deployment loop runs on infrastructure the user owns, so the agent must be inspectable and
hard to misuse. The intended model:

- The agent **generates a keypair on install**; the control plane stores the **public** key.
- Registration uses a **one-time token**, exchanged for durable, **rotatable** credentials.
- The control plane sends **signed** jobs; the agent **validates the signature and the job's
  scope before executing**.
- The connection is **outbound from the agent** — the control plane never needs to hold a
  long-lived root SSH key to the server, and never needs inbound SSH after setup.
- Every job the agent runs is **audited** (see [security.md](./security.md)).

> [!NOTE]
> Avoid designs that reintroduce long-lived root SSH keys stored in the control plane, or that
> let the agent run unsigned/unscoped work. Those are exactly the trust problems this model
> exists to remove.

## Caddy ownership

The agent owns Caddy's desired state on its server. The loop is:

1. Generate the Caddy config from the platform's desired state.
2. **Validate** the config.
3. Reload Caddy.
4. Report success/failure to the control plane.

This is also how traffic is switched during a deploy or rollback — see
[deployment-engine.md](./deployment-engine.md).

## Updates & reconnection

The agent is designed to reconnect cleanly after a restart or a network drop, and to support a
safe self-update mechanism (apply, verify, and roll back the agent itself if an update fails).
Keep these paths conservative: a broken agent update must never take a server's apps down.

## Scope

This doc covers the agent's **mechanism** — how it connects and what it operates. The set of
*things* the platform can deploy or manage grows over time; for that scope see
[ROADMAP.md](../../ROADMAP.md) rather than expanding lists here.
