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

A deployment is one deploy attempt **of a service** (the deployable component — see
[data-and-api.md](./data-and-api.md)); it inherits that service's source, port, and
visibility. Creating a service with `deploy_now` enqueues the first deployment;
`DeploymentService.CreateDeploymentForService` enqueues a **redeploy** of an existing one, and
`DeploymentService.CreatePreviewDeployment` enqueues a **branch/PR preview** that runs alongside
production (see [Preview deployments](#preview-deployments)).

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
> **What's built so far.** A deployment carries its service's **source kind**:
> - **`image`** — a pre-built public image. The agent claims it (steps 1–3), pulls the image,
>   joins the container to its [per-environment network](#visibility--per-environment-networking),
>   and — for a **public** service — also publishes a **host port**, health-checks it,
>   validates/reloads a Caddy route to that host port, supersedes the **service's** previous
>   container, and reports status + logs (steps 6–11).
> - **`git`** — a **public** repository. The agent additionally **clones** the repo (step 3 —
>   an anonymous shallow clone, no credential) and **builds it with BuildKit** (steps 4–5:
>   `docker build` with `DOCKER_BUILDKIT=1`) into a local image, then runs that image (steps
>   6–11). When the repo ships its own **Dockerfile** the agent builds that; when it doesn't,
>   **build detection** (see below) identifies a supported framework (Node, Vite, Next.js) and
>   **generates a Dockerfile** to build instead. It reports two extra phases, `cloning` and
>   `building`, streams the build output (including any generated Dockerfile) as log lines, and
>   records the exact `commit_sha` and `built_image_ref`. A repo that is neither Dockerfile-based
>   nor a recognized framework is a clear deployment failure with next steps. The **container
>   port is optional**: when the request omits it (`container_port = 0`), the agent reads the
>   built image's `EXPOSE` (via `docker image inspect`, so it also sees a base image's exposed
>   port — and a generated Dockerfile always sets one) and publishes the lowest TCP port; if the
>   image exposes none, the deployment fails asking for an explicit port.
>
> Build detection covers a **repo Dockerfile** and, when there is none, a **generated Dockerfile
> for a detected Node/Vite/Next.js app** (the shared `internal/builder` package — see below).
> **Public and GitHub App-backed private repos are built**: a `public` source clones anonymously,
> while an `app` source is cloned with a short-lived installation token minted per claim and handed
> to the agent in the job (an `oauth` source stays discovery-only — see
> [security.md](./security.md) and [sources.md](./sources.md)). Logs are
> delivered by **polling**, not SSE; Caddy routing is
> HTTP-only and derives a route from the **service id** (so two services in one environment
> don't collide). SSL, custom domains, Compose/Nixpacks/static builds, and one-click rollback
> are later slices. The claim is atomic per server (a queued deployment is the unit of work; a
> general job queue comes later).

## Build priority

Detect and build in this order:

1. **Dockerfile**, if present.
2. **Docker Compose**, if present.
3. **Detected framework** (Node, Vite, Next.js, …) → a **generated Dockerfile**.
4. **Static-site** fallback.
5. **Manual** build/start command.

Every build path ends in a **Dockerfile built with BuildKit** — a detected framework gets a
small generated Dockerfile rather than a separate buildpack runtime, so there is one build
mechanism and the generated file is previewable. We do **not** run Nixpacks or a custom
buildpack system.

> [!NOTE]
> **Implemented so far: a repo Dockerfile, plus generated Dockerfiles for Node/Vite/Next.js.**
> The agent builds with BuildKit (`docker build`, `DOCKER_BUILDKIT=1` — so the prepared server
> needs the Docker CLI, which the standard install provides). When the repo has no Dockerfile,
> the agent runs the shared `internal/builder` detection over the clone, writes a generated
> `Dockerfile.plorigo`, and builds that. Compose, static-site, and manual builds are still
> deferred. A repo that is neither Dockerfile-based nor a detected framework fails with a
> plain-English message and next steps.

## Build & framework detection

The engine inspects a repo to determine a build command, start command, port, and runtime, and
renders a Dockerfile from them. Detection lives in the shared **`internal/builder`** package
(stdlib-only, no DB or transport) so the **agent** runs it over the cloned tree at build time
and the **control plane** runs the *same* rules over the GitHub contents API — what the
dashboard previews via `ServiceService.DetectFramework` is exactly what the agent builds. It
starts with the basics (Node, Vite, Next.js) and grows from there.

> [!NOTE]
> Keep detection lists out of this doc. The set of frameworks Plorigo recognizes is product
> scope — see [ROADMAP.md](../../ROADMAP.md). The rules and Dockerfile templates live as data in
> `internal/builder`; **that package is the catalogue**, not this document.

## Runtime: Docker first

The runtime target is **Docker Engine**, on one server, first. **Kubernetes is intentionally
out of scope** for the initial design — the first users want simple production on a VPS or
bare metal, not cluster operations. If multi-server or orchestration needs become real, that's
a roadmap decision, not an assumption to build in now.

## Rollback

Rollback should, wherever possible, be a **route switch back** to the previous healthy
container — fast and low-risk because the old version is still around within the retention
window. Supersede is **per service**: a new running deployment replaces only that **service's**
previous running container on the server, so deploying one service never disturbs a sibling.
The failed deployment's logs and diff stay available so the failure can be understood (and, per
[principles.md](./principles.md), every scary action keeps a recovery path).

## Reverse proxy & SSL (Caddy)

[Caddy](https://caddyserver.com/) is the reverse proxy and SSL terminator:

```text
Internet → Caddy (on the user's server) → Docker network → app containers
```

Caddy handles app routing, preview URLs, custom domains, automatic HTTPS, HTTP→HTTPS
redirects, WebSocket proxying, and the route switch during deploy/rollback. The **agent** owns
generating, validating, reloading, and reporting Caddy config — details in [agent.md](./agent.md).

The Caddy **route key is the deployment's `route_key`**, which for a production deployment is
the **service id**: the generated host is `{service-id}.{baseDomain}` (it was
`{env-id}.{baseDomain}`), so two services in one environment get distinct routes. The control
plane carries this to the agent as the job's **`app_label`**, and the agent stamps it on the
container as the `plorigo.service` label — what it matches on to find and supersede the
previous container with the same key. A **preview** deployment overrides `route_key` so it gets
its own host and replacement group (see [Preview deployments](#preview-deployments)). Only a
**public** service gets a route at all (see below).

Custom domains layer onto that generated route. Users attach one or more hostnames to a
service; the dashboard shows the exact DNS record to point at the generated host and verifies
DNS before routing. The outbound agent periodically asks the control plane for verified
custom hostnames for the managed containers it is currently running, renders those hostnames
beside the generated host in the Plorigo-managed Caddyfile, and reports route-sync failures
back to the domain rows. This slice is HTTP-only; automatic HTTPS/SSL is deferred.

## Visibility & per-environment networking

Every service container joins a **per-environment Docker network** named
`plorigo-{environment-id}`, with a `--network-alias` equal to the service's **slug**. A sibling
in the same environment therefore reaches it at `http://{slug}:{container_port}` — east-west
traffic stays on the private network and never goes through Caddy. The slug is unique within
the environment (it is the service's DNS label), so aliases don't collide.

A service's **visibility** decides what is exposed to the outside:

- **`public`** — additionally publishes a **host port** and gets a **Caddy route + public
  URL** (`route_url`). This is the front door (a web app, an API).
- **`private`** — publishes **nothing** to the host and has **no Caddy route** (so no
  `route_url`). It is reachable only by its siblings over the per-environment network — the
  shape for a database, cache, or internal worker.

## Preview deployments

A **preview** is a deployment of a service built from a **branch or pull request** that runs
**alongside** the service's production deployment — Plorigo's take on Vercel-style previews,
without a separate environment or service row. `DeploymentService.CreatePreviewDeployment`
enqueues one: it resolves the service's source server-side (a `public` or GitHub App-backed
`app` git service — an `oauth` source is not buildable) and takes either a `branch` or a
`pr_number` (resolved through the GitHub API to its head ref, and linked back via `pr_url`). A
private (`app`) preview is cloned with the same per-claim installation token as a production deploy.

A deployment carries a **`kind`** (`production` | `preview`) and a **`route_key`**. The
`route_key` is what isolates a preview, because it is what the deploy flow keys on in the three
places that were previously keyed by service id:

- **Routing** — the agent's `app_label` is the `route_key`, which keys the container's label,
  replacement group, and supersede scope. Production keeps `route_key = {service-id}` and its host
  is `{service-id}.{baseDomain}` (unchanged). A preview gets a distinct, DNS-safe `route_key`
  (`{service-id}-pr-{n}`, or a slug of the branch, truncated with a hash tail when long) so it
  never fights production for the route — but its **public host is a human-readable**
  `{slug}-pr-{n}-{hash}.{baseDomain}` (the control plane sends this `route_host` to the agent; the
  short hash of the service id keeps it collision-safe across services that share a slug). So the
  internal key stays UUID-stable while the URL reads nicely.
- **Supersede** — a new running deployment supersedes only the prior one with the **same
  `route_key`**, so re-pushing a preview replaces that preview, and a preview never supersedes
  production (or another PR's preview).
- **Network isolation** — a preview joins its **own** network `plorigo-preview-{route_key}`
  rather than the per-environment network, so it **cannot reach production's siblings** (e.g.
  the production database). The agent creates that network lazily, like any other.

A preview is isolated further by **withholding the environment's secrets**: it receives the
service's non-secret variables but not decrypted secrets, since it builds untrusted branch/PR
code. The service's cached live `route_url` always tracks **production**, never a preview. None
of this requires an agent change — the agent already derives the route, the replacement group,
and the network from the job's `app_label` and `network_name`, which the control plane sets.

### Teardown

A preview can be **removed on demand** — `DeploymentService.TeardownPreview` (by preview
deployment id) enqueues a **teardown job** for the preview's server agent, mirroring the
backups/restore job model (a `teardown_jobs` table + the agent-facing `agent.v1.TeardownService`
Poll/Report, rather than overloading the deployment poll). The agent stops + removes every
container labelled `plorigo.service={route_key}`, then **reconciles Caddy from Docker truth**
(`listManagedRoutes` → `router.apply`), so the route drops automatically because the container is
gone. Teardown is **idempotent** (an already-removed preview reports success), it best-effort
removes the preview's isolated `plorigo-preview-{route_key}` network, and it is **scoped to the
`route_key`** — production and other previews are never touched. On success the control plane
moves the preview's deployment rows to a terminal **`torndown`** status (so the dashboard stops
showing it running) and the action is audited. Failures surface on the preview row in the
dashboard's Previews panel, where teardown is triggered by a **Remove preview** action.

### Webhook-driven previews

Previews also run **automatically from GitHub pull-request events**. The **GitHub App** (see
[security.md](./security.md)) delivers `pull_request` webhooks to `POST /api/github/webhook`; the
handler **verifies the HMAC signature** over the raw body against `GITHUB_WEBHOOK_SECRET` **before
parsing** (fail-closed — an unset secret rejects every delivery), then hands the event to the
`internal/webhooks` module. That module **re-scopes** the delivery: it maps the `installation_id`
→ the workspace that connected it, then the repo `owner/repo` → that workspace's matching git
services, so a verified event can only ever touch what the installation legitimately owns. For each
matched service it calls the deployments seam — `opened` / `synchronize` / `reopened` →
`CreatePreviewForPR` (keyed by PR number, so a re-push **supersedes** the same preview), `closed`
(merged or not) → `TeardownPreviewForPR` (phase-2 teardown). These seam methods are **not
policy-authorized** — the signature check + the installation→workspace→service mapping is the gate
(like `EnqueueFirstDeployment`) — and they are **idempotent**: a re-push supersedes, a teardown of
an already-gone preview is a no-op, and an unknown installation / unmatched repo / unhandled action
is ignored. A per-service failure is logged and skipped so one bad service neither drops the others
nor makes GitHub redeliver.

**Expiration.** Abandoned previews don't linger: the control plane runs a periodic **expiry
sweep** that tears down (via the same teardown job) every **running** preview older than a
configurable TTL (`PLORIGO_PREVIEW_TTL_HOURS`, default 72h; `0` disables it). The sweep is a
system action — not policy-authorized, audited with a `preview-expiry` actor — and idempotent
(a torn-down preview leaves `running`, so a later sweep skips it).

**Password protection.** A preview URL can require a **password** (basic auth) so a not-yet-public
preview isn't world-reachable. The control plane **bcrypt-hashes** the password at create time and
stores only the hash (never the plaintext); the username is validated to a safe charset. The hash +
username travel to the agent in the deploy job and are stamped as container labels, so the agent
renders a `basic_auth` block on the preview's Caddy route (and rebuilds it from Docker truth on the
route-sync loop). Production routes and unprotected previews render no `basic_auth`.

> [!NOTE]
> **What's built so far.** Previews are created **manually** (dashboard or RPC) **and
> automatically from PR webhooks** (above), for **public** git services; they are **torn down on
> demand, on PR close, or by the TTL expiry sweep**. Building a **private** repo through the App's
> installation token builds on this and is a later slice — see [ROADMAP.md](../../ROADMAP.md).
> Note the two senses of "preview": an **environment** of
> `type = 'preview'` (a long-lived environment) is distinct from a **deployment** of
> `kind = 'preview'` (the ephemeral branch/PR build described here).

## Managed database services

A **managed database** (Postgres today) is provisioned as a `template` service from a small,
**control-plane-owned catalogue** (`internal/services/templates.go` — there is no template
registry table): the control plane fixes the image and port, forces **`private`** visibility (a
database must never get a public route), resolves the credentials, and stores them as the
service's **config variables** so the agent injects them when it starts the container.
Credentials + the service + the first deployment commit in one transaction (the config is
written through a consumer-defined `ConfigSetter` port, the same shape as the deploy
`Enqueuer`). Siblings connect over the per-environment network at `{slug}:{port}`; the
connection URI is returned once at create time and the dashboard also rebuilds it from the
stored config.

**Caller options.** `CreateDatabaseService` accepts an optional **database name**, **username**,
and **password**; each falls back to the template default, and a blank password is generated
(`crypto/rand`). The image and port stay control-plane-chosen, so a caller can't smuggle an
arbitrary image through the database path. The database name and user are validated as plain
identifiers (≤ 63 chars); a supplied password is length-bounded.

**Dashboard.** A managed database is one **template** in the unified "Add a service" gallery
(`apps/web/src/lib/templates.ts`), alongside the image and repo templates — there is no separate
database lane. Choosing any template opens a **"Configure & deploy" dialog**
(`TemplateConfigDialog`) that renders the template's declared `options` (for Postgres: database
name, username, a **generated password** the user can reveal, copy, edit, or regenerate, and a
read-only port), then creates and deploys.

**Adding a managed service** (e.g. MongoDB, Redis) is a data-only change: add a
`databaseTemplate` entry — image, port, scheme, default user/db — plus a case in
`env()`/`connectionURI()` (`internal/services/templates.go`), and a matching `managed` template
with its `options` in `apps/web/src/lib/templates.ts`. No new RPC, table, or agent change.

> [!NOTE]
> This is the **basic** path. Data is **not yet persisted across redeploys** — the agent does
> not mount volumes, so a redeploy starts a fresh database (persistent volumes are a later
> slice; see [ROADMAP.md](../../ROADMAP.md)). Credentials (including a user-supplied password)
> live in the service's (plaintext, readable) config variables rather than the sealed-secret
> path, because secret *injection* at deploy time isn't wired yet — moving managed credentials
> onto that path is a follow-up.
