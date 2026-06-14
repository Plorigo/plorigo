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

> [!NOTE]
> **What's built so far.** A deployment has a **source kind**:
> - **`image`** — a pre-built public image. The agent claims it (steps 1–3), pulls the image,
>   starts the container on a published **host port**, health-checks it, validates/reloads a
>   Caddy route to that host port, retains/replaces the previous container, and reports status
>   + logs (steps 6–11).
> - **`git`** — a **public** repository. The agent additionally **clones** the repo (step 3 —
>   an anonymous shallow clone, no credential) and **builds its Dockerfile with BuildKit**
>   (steps 4–5: `docker build` with `DOCKER_BUILDKIT=1`) into a local image, then runs that
>   image (steps 6–11). It reports two extra phases, `cloning` and `building`, streams the build
>   output as log lines, and records the exact `commit_sha` and `built_image_ref`. A missing
>   Dockerfile is a clear deployment failure. The **container port is optional**: when the
>   request omits it (`container_port = 0`), the agent reads the built image's `EXPOSE` (via
>   `docker image inspect`, so it also sees a base image's exposed port) and publishes the
>   lowest TCP port; if the image exposes none, the deployment fails asking for an explicit port.
>
> Build detection is **Dockerfile-only** for now (a one-file check on the agent; the `builders`
> module below is still deferred), and **private repos aren't built yet** — only `access =
> 'public'` sources are dispatched, so no credential ever leaves the control plane (see
> [security.md](./security.md)). Logs are delivered by **polling**, not SSE; Caddy routing is
> HTTP-only and derives a route from the environment id. SSL, custom domains, Compose/Nixpacks/
> static builds, and one-click rollback are later slices. The claim is atomic per server (a
> queued deployment is the unit of work; a general job queue comes later).

## Build priority

Detect and build in this order:

1. **Dockerfile**, if present.
2. **Docker Compose**, if present.
3. **Nixpacks** fallback for common apps.
4. **Static-site** fallback.
5. **Manual** build/start command.

Dockerfile builds use **BuildKit** underneath. We do **not** build a custom buildpack system.

> [!NOTE]
> **Implemented so far: Dockerfile only.** The agent builds a Dockerfile at the repo root with
> BuildKit (`docker build`, `DOCKER_BUILDKIT=1` — so the prepared server needs the Docker CLI,
> which the standard install provides). Detection is a one-file check on the agent for now, not
> the `builders` module yet; Compose, Nixpacks, static-site, and manual builds are deferred. No
> Dockerfile → the deployment fails with a plain-English message.

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
