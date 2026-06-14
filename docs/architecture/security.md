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
- **Environment-scoped, deliberately.** Plain `env_vars` are **service-scoped** (each service is
  its own app), but **secrets stay environment-scoped** this round — a conscious asymmetry; a
  follow-up may align them. See [data-and-api.md](./data-and-api.md).

See [data-and-api.md](./data-and-api.md) for how secret ciphertext and metadata are stored.

### Source integration credentials (GitHub OAuth, public repos)

Pointing a **service** at a Git repository may store a provider credential, so it follows the
secret discipline with one deliberate difference — the token is **opened server-side** to call
the provider on the user's behalf. How a source is reached is recorded in an explicit `access`
discriminator on the service's folded source (`oauth` | `public` | `app`; see
[data-and-api.md](./data-and-api.md)):

- The OAuth **App** credentials (`GITHUB_OAUTH_CLIENT_ID` / `GITHUB_OAUTH_CLIENT_SECRET`) are
  **server config**, not per-workspace data; when unset, the connect flow reports itself as not
  configured and the UI disables it (the public path stays available regardless).
- The per-workspace OAuth **access token** (`access = 'oauth'`) is **sealed at rest** with the
  same AES-256-GCM box and `APP_MASTER_KEY` as secrets. It is **write-only through the API**: no
  RPC returns it and it is **never logged**. It is decrypted only in-process to call the provider
  (list repositories, read a repo/branch). It is **never sent to the agent**: building from a
  private/OAuth repo is **not implemented**, so creating a service with a non-public git source
  is rejected. A private build will use a short-lived **GitHub App installation token** minted
  per job, not this broad, long-lived OAuth token.
- **Public repositories (`access = 'public'`) carry no credential at all.** The repo is read
  **unauthenticated** (empty token, no connection), so only genuinely public repos resolve — a
  private or missing repo is invisible to an anonymous request and surfaces as "not found". The
  service stores a **NULL connection**, so it is never blocked by — and never blocks — a
  workspace's OAuth connection. This is also the **only** source kind built-and-deployed today:
  the control plane hands the agent just the **clone URL** (no token), and the agent does an
  anonymous shallow clone before building. The lowest-blast-radius way to deploy an open-source app.
- One OAuth connection per workspace (`source.connect` / `source.read` / `source.disconnect`
  actions), authorized **before** the write and **audited** in the same transaction. Disconnecting
  the provider is blocked while services still reference it (a recovery path, not a silent
  cascade); public sources hold no connection and so never participate in that guard.
- The browser OAuth handshake is protected by a **sealed, expiring, single-use `state`** bound to
  the initiating workspace and user (set as an `HttpOnly` cookie, echoed back and verified on the
  callback) — the callback is the one cross-site entry point and must not act on a forged request.
- **Disconnect (and reconnect) revoke the token at GitHub** (best-effort `DELETE
  /applications/{client_id}/token`), so "disconnect" actually cuts off access rather than only
  forgetting the token locally — OAuth-App tokens do not expire on their own.

> [!IMPORTANT]
> **Blast radius.** The OAuth-App scope defaults to `repo` (`GITHUB_OAUTH_SCOPES`), which grants
> read/write to **all** of the connecting user's repositories — there is no per-repo or read-only
> grant with an OAuth App. The token is encrypted at rest and revoked on disconnect, but a
> highly-privileged credential still exists while connected. Set `GITHUB_OAUTH_SCOPES=public_repo`
> to limit it to public repositories. For an open-source app, the **public-repo path** avoids the
> credential entirely (connect by URL, read anonymously) — and public **clone/build already needs
> no credential**, since the agent only ever receives a clone URL. The narrower, per-repo,
> read-only path for **private** clone/build is a **GitHub App** (a short-lived installation token
> minted per job), which is the direction before private builds land — see
> [ROADMAP.md](../../ROADMAP.md).

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
