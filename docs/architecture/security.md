# Security model

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

> [!IMPORTANT]
> This is **engineering design**. To **report a vulnerability**, follow [SECURITY.md](../../SECURITY.md)
> — do not open a public issue. Read this doc before touching secrets, audit, permissions,
> approvals, the agent, the terminal, or the AI/MCP gateway.

Plorigo controls servers, containers, secrets, databases, and terminals, so security is
foundational — not a feature. The governing principle: **own-server should not mean
unsafe-server** (see [principles.md](./principles.md)).

## Part 1 — Platform security model

### Least privilege & container safety

- **Principle of least privilege** for agents and jobs: send only what a specific job needs.
- **Never expose the Docker socket to user apps.**
- **Detect, warn on, or block** privileged containers, host networking, and dangerous mounts.
- Apply policy checks (the `policy` module) **before** risky actions run.

### Authentication & authorization

The realized seam is documented in [auth.md](./auth.md). In short: one ConnectRPC
interceptor resolves the caller (session cookie or bearer API token) into a
`principal` and rejects unauthenticated calls to non-public procedures; each
privileged service then calls `policy.Authorize` **before** mutating and audits the
real actor in the same transaction. Session and API tokens are stored **hashed**
(never raw), session cookies are `HttpOnly`/`SameSite=Lax`/`Secure`-in-production,
cookie requests carry a CSRF guard, and password reset revokes all sessions. Raw
tokens and reset/verify links must never reach the audit trail or logs.

### Secrets

- **Encrypted at rest.** Self-host uses a master key (`APP_MASTER_KEY`); a managed deployment
  can use KMS/Vault-style key management later.
- **Redacted in logs** — always.
- **Write-only in the UI** where possible; **versioned**; every create/update/delete is **audited**.
- **Build-time and runtime secrets are separated.**
- **Scoped per job** — a deployment job receives only the secrets it requires, never the whole set.

See [data-and-api.md](./data-and-api.md) for how secret ciphertext and metadata are stored.

### Audit, approvals, recovery

- An **append-only audit log** records deploys, secret changes, terminal sessions,
  backup/restore, and permission changes.
- **Production deploys can require approval**; **production migrations require a backup first**.
- Every scary action keeps a **recovery path** (rollback, restore) — see [principles.md](./principles.md).

### Terminal

The web terminal is **permission-gated**, **audited**, shows a **production warning**, and is
**disabled for AI agents**.

## Part 2 — AI / MCP agent safety

AI agents can help operate apps, but must not be trusted blindly. The AI/MCP gateway exposes
tools in **tiers**, and the policy is enforced by the `policy`/`mcp` modules — not left to a
prompt.

| Tier | What it allows | Examples (illustrative) |
|---|---|---|
| **Read-only** | Observe, never change | read deployment status, build/runtime logs, required env vars, readiness report |
| **Low-risk write** | Safe, reversible changes | create a preview deployment, set a non-secret env var |
| **Approval-required** | Sensitive actions, human approves first | deploy to production, run a migration, restore a backup, change a domain |
| **Forbidden** | Never allowed via the gateway | delete a production database or volume, read a raw secret value, disable backups, open a production terminal |

> The policy line, stated plainly:
>
> **Your AI can read logs and suggest fixes. It cannot delete production data.**

Rules for anyone working here:

- The tiers above are the **policy model**, not a published API surface. The concrete tool list
  is product scope — see [ROADMAP.md](../../ROADMAP.md). Add tools to the right tier; never
  quietly promote one to a weaker tier.
- AI agents **cannot** read raw production secrets, delete databases/volumes, disable backups,
  deploy to production without approval, run production migrations without approval **and** a
  backup, or open a production terminal.
- **All** agent actions are audited, and agent access is **visible and revocable**.

Any change that broadens what an AI agent — or an unprivileged user — can do gets extra review
(see [conventions.md](../conventions.md) and [CONTRIBUTING.md](../../CONTRIBUTING.md)).
