# Plorigo Roadmap

Plorigo is being built in phases, from a working server-agent deployment loop toward a polished,
open-source, production-safe deployment platform with an optional managed cloud.

> [!NOTE]
> This roadmap is directional, not a commitment to dates or ordering. The best way to influence
> it is to join the conversation in [Discussions](https://github.com/Plorigo/plorigo/discussions).

## North star

> Get the deployment experience you wanted from Vercel, the ownership you wanted from Coolify,
> and the safety you need for apps built by humans **or** AI.

## Phases

### Phase 0 — Validation & product design
User interviews, clickable prototype, architecture decisions, brand. *Define the wedge.*

### Phase 1 — Technical prototype
Prove the server-agent deployment loop: connect a VPS with one command, deploy a GitHub repo,
build & run a container, route via reverse proxy, basic logs, basic domain/SSL.

### Phase 2 — Private alpha MVP
Make it useful for real small apps: project/environment model, GitHub import, Dockerfile support,
framework detection, env vars/secrets, custom domains + SSL, deployment timeline, health checks,
one-click rollback, basic Postgres + backups, server health, basic Production Readiness Doctor,
plain-English failure summaries.

### Phase 3 — Previews & dashboard polish
PR/branch preview deployments, preview URLs + expiration, password-protected previews, searchable
logs/env vars, `.env` import, better rollback UX, external Postgres/Redis linking, Redis service,
basic alerts.

### Phase 4 — AI Builder Mode
"Deploy an AI-built app" onboarding, import from GitHub/zip and AI tools (Cursor, Lovable, Bolt,
Replit, Claude Code, Windsurf, v0), app explanation page, required-env detection, `.env.example`
generation, plain-English errors, copy-paste fix prompts, Supabase/Stripe/Auth checkers,
production readiness score, production lock.

### Phase 5 — Agencies, teams & safer production
Team invites, project/environment-level roles (incl. ownership transfer & demotion),
isolated workspace wizard, resource quotas,
client/viewer role, backup/restore center, volume backups, restore to a new server, persistent
terminal sessions, approval workflows for production deploys/migrations, basic cost/capacity.

### Phase 6 — Safe AI-agent operations
MCP server with read-only tools first, then low-risk preview tools, then approval-required
production tools; agent permission UI + audit log; staging DB clone; migration safety flow.
*AI can read logs and suggest fixes — it cannot delete production data.*

### Phase 7 — Public cloud beta & commercial validation
Hosted control plane beta, managed updates, billing foundation, Cloud Starter/Pro packaging,
secure multi-tenant review, cloud onboarding. The OSS self-hosting path stays first-class.

### Phase 8 — Public open-source v1 & stable monetization
Polished public repo, clear license, install/upgrade/dev docs, example apps, docs site, a clean
free/paid boundary, and the hosted Cloud + Team/Agency/Enterprise plans.

## Open-core: what's free vs paid

We will **not** meter your app traffic. The deployment core is open source and genuinely useful
on its own; paid plans are about convenience, scale, and team/agency/enterprise workflows.

**Free, open-source core (self-hosted):** server agent, deployment engine (Docker/BuildKit),
Caddy/proxy + SSL, Git deploys, project/environment/service model, domains, build/runtime logs,
env vars + encrypted secrets, basic teams, basic backups/restores, rollbacks, Postgres/Redis
templates, basic Production Readiness Doctor, CLI, Docker Compose self-hosting.

**Paid cloud / enterprise (convenience & scale):** hosted control plane + managed updates,
advanced RBAC & approval workflows, long log/audit retention, advanced backup monitoring &
scheduled restore tests, cross-server restore, AI-agent/MCP permission controls, white-label
agency/client portals, resource quotas & usage reports, SSO/SAML/SCIM, compliance reports, and
priority/enterprise support.
