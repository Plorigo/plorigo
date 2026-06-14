# Dashboard

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

The dashboard (`apps/web`) is a **product UI**, not a content site — fast local development,
realtime logs, forms, tables, a command palette, deploy timelines, and terminal sessions. Read
this before working in `apps/web`.

## Stack

| Concern | Choice |
|---|---|
| Framework | **React + TypeScript + Vite** |
| Routing | **TanStack Router** |
| Server state | **TanStack Query** |
| Styling | **Tailwind CSS** |
| Components | **shadcn/ui** |
| Small client-only state | **Zustand** or **TanStack Store** |

**Decision:** do **not** use Next.js for the core dashboard. It's an authenticated app behind
the control plane, not an SSR content site. (A marketing/docs site, if one is added later, is a
separate concern and can make its own choice.)

## Talking to the backend

- The dashboard calls the control plane through the **generated ConnectRPC client** — see
  [data-and-api.md](./data-and-api.md). Don't hand-roll fetch wrappers around the API.
- **Auth** ([auth.md](./auth.md)): the transport sends the session cookie
  (`credentials: "include"`); a route guard redirects to `/login` when unauthenticated,
  while the auth screens (login, register, forgot/reset, verify) are public.
- Realtime comes over **SSE** (deploy status, logs) and **WebSockets** (terminal) — see
  [jobs-and-realtime.md](./jobs-and-realtime.md).

## UI conventions

- **Progressive disclosure.** Lead with a clear, plain summary; keep the raw details (full
  logs, config, Docker specifics) one click away. Beginners aren't overwhelmed; power users
  are never blocked. This is a core [principle](./principles.md).
- **Every deploy has a timeline; every failure has a summary; every risky action has a
  recovery path.** Build UI that reflects that.
- Keep components **typed and accessible**, and follow the project ESLint/Prettier config —
  see [conventions](../conventions.md).

> [!NOTE]
> **Service-centric structure.** Now that a **service** is the deployable unit (see
> [data-and-api.md](./data-and-api.md)), the UI follows the same shape: the project page lists
> the project's **services** (the old deployments-table + source panel are gone), and its
> primary action is **Add service** rather than "Deploy". The full-page picker at
> `/deployments/new` now **creates a service** — it adds a service-name field and a
> public/private visibility toggle and calls `ServiceService.CreateService(deploy_now)`. A
> **service detail page** at `/projects/$projectId/services/$serviceId` shows the source, port,
> visibility, env vars, and deployment history, with a **Redeploy** action
> (`CreateDeploymentForService`). Env-var management is **service-scoped**.

> [!NOTE]
> Beyond the structure above, specific screens and navigation are product scope, not
> architecture — see [ROADMAP.md](../../ROADMAP.md). This doc is about the stack and the
> conventions every screen follows.
