# Deployment engine

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

The engine turns "deploy this" into a running, reachable app — reliably, observably, and
reversibly. Read this before changing build, deploy, rollback, runtime, or proxy code.

## The deploy flow

1. A Git event or manual action **triggers** a deploy.
2. The control plane creates a **deployment record** and a **job**.
3. The **agent** receives the signed job and **fetches the source**.
4. **Build detection** runs (see below).
5. The image is built with **BuildKit** (or the Nixpacks fallback).
6. The agent starts a **new container on an isolated Docker network**.
7. A **health check** runs against the new container.
8. **Caddy switches the route** to the new version.
9. The **previous version is retained** for the rollback window, then stopped after a grace period.
10. The deployment is marked successful; **logs and metrics keep streaming**.

Jobs and streaming are described in [jobs-and-realtime.md](./jobs-and-realtime.md); the agent
side of Caddy and container management is in [agent.md](./agent.md).

## Build priority

Detect and build in this order:

1. **Dockerfile**, if present.
2. **Docker Compose**, if present.
3. **Nixpacks** fallback for common apps.
4. **Static-site** fallback.
5. **Manual** build/start command.

Dockerfile builds use **BuildKit** underneath. We do **not** build a custom buildpack system.

## Build & framework detection

The engine inspects a repo to suggest a build command, start command, port, and the likely
runtime — starting with the basics (Node, static sites, Dockerfile, Docker Compose) and
growing from there.

> [!NOTE]
> Keep detection lists out of this doc. The set of frameworks and integrations Plorigo
> recognizes is product scope — see [ROADMAP.md](../../ROADMAP.md). Implement detection as
> data/rules in the `builders` module, not as a catalogue duplicated into documentation.

## Runtime: Docker first

The runtime target is **Docker Engine**, on one server, first. **Kubernetes is intentionally
out of scope** for the initial design — the first users want simple production on a VPS or
bare metal, not cluster operations. If multi-server or orchestration needs become real, that's
a roadmap decision, not an assumption to build in now.

## Rollback

Rollback should, wherever possible, be a **route switch back** to the previous healthy
container — fast and low-risk because the old version is still around within the retention
window. The failed deployment's logs and diff stay available so the failure can be understood
(and, per [principles.md](./principles.md), every scary action keeps a recovery path).

## Reverse proxy & SSL (Caddy)

[Caddy](https://caddyserver.com/) is the reverse proxy and SSL terminator:

```text
Internet → Caddy (on the user's server) → Docker network → app containers
```

Caddy handles app routing, preview URLs, custom domains, automatic HTTPS, HTTP→HTTPS
redirects, WebSocket proxying, and the route switch during deploy/rollback. The **agent** owns
generating, validating, reloading, and reporting Caddy config — details in [agent.md](./agent.md).
