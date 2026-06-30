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
  (list repositories, read a repo/branch). It is **never sent to the agent**: an OAuth source is
  **discovery only** (browse + import), so an `access = 'oauth'` service is recorded but **not
  built** — this broad, long-lived token never leaves the control plane. **Private builds** use the
  narrower, short-lived **GitHub App installation token** minted per use (below), not this token.
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

#### GitHub App (private reads + webhook verification)

A workspace can also connect a **GitHub App installation** (`provider = 'github_app'`), the
narrower, per-repo, read-only path for **private** repositories and the basis for webhook-driven
previews. Its credential model is deliberately tighter than OAuth's:

- The **App private key**, **app id**, **slug**, and **webhook secret** are **server-wide config**,
  held only in the control plane. They come from one of two places, **env first**: the
  `GITHUB_APP_*` environment variables (operator-set, takes precedence), or — when those are unset —
  a **sealed singleton row** (`github_app_config`) written by the dashboard's **automated
  registration** (GitHub's App-manifest flow). When stored, the private key, webhook secret, and
  OAuth client secret are **sealed at rest** (AES-256-GCM, `APP_MASTER_KEY`) and **write-only**: no
  RPC returns them, they are never **logged**, and the private key/secret are never **sent to the
  agent**. Registering is **owner-only** (`github_app.register`) since the app serves every
  workspace; it is **audited**, and a DB leak exposes only ciphertext. When neither source is
  configured, the App features report themselves as not configured and the UI offers registration.
- **No long-lived token is stored.** Unlike OAuth, an App connection stores only the
  **`installation_id`** (and the account login, for display) — never a token. A workspace may hold
  **many** App connections (one per org it installs on) + OAuth connections; a service references the
  specific connection it builds from. To read a private repo, the control plane signs a short-lived
  **RS256 app JWT** with the private key (≤10-minute lifetime, held in memory only) and exchanges it
  for that connection's **per-installation access token** that
  GitHub expires within the hour. The token is **cached in memory** until shortly before expiry,
  used in-process to call GitHub (repo/branch listing, validation), and **never persisted or
  returned by an RPC**. So a database leak exposes no GitHub credential for an App connection, and
  the blast radius is one installation's granted repositories for at most an hour.
- **Private builds hand the agent a fresh token, scoped to one deploy.** To clone a private
  (`access = 'app'`) repo, the control plane mints an installation token **when the agent claims the
  deploy** and includes it in the signed job as `git_credential`. The agent applies it as the
  password of an `x-access-token` HTTP basic-auth — it is **never embedded in the clone URL** (so it
  can't leak into a log line or the stored URL), never written to the build log, and discarded after
  the clone. This is the one credential that does reach the agent, and only for an App source: it is
  short-lived (≤1h), single-deploy, and travels over the agent's TLS transport. The broad OAuth
  token is still never sent. If no installation is connected when a private deploy is claimed, the
  deployment is **failed** with a recoverable message instead of falling back to an anonymous clone.
- The install handshake reuses the OAuth flow's protection: a **sealed, expiring, single-use
  `state`** bound to the initiating workspace + user (an `HttpOnly` cookie), verified on the setup
  callback (`/api/github/app/setup`) before the installation is recorded and **audited**.
- **Inbound webhooks** drive previews from PR events (`POST /api/github/webhook`). The handler
  verifies `GITHUB_WEBHOOK_SECRET` via a constant-time **HMAC-SHA256** check of the raw body against
  `X-Hub-Signature-256` **before parsing**; a missing or mismatched signature is rejected, and an
  unset secret rejects every delivery (**fail-closed**). A verified event is **re-scoped** to the
  installation's workspace and only that repo's services, so the one external entry point that
  drives deployments can never reach beyond what the installation owns. See
  [deployment-engine.md](./deployment-engine.md).

> [!IMPORTANT]
> **Blast radius.** The OAuth-App scope defaults to `repo` (`GITHUB_OAUTH_SCOPES`), which grants
> read/write to **all** of the connecting user's repositories — there is no per-repo or read-only
> grant with an OAuth App. The token is encrypted at rest and revoked on disconnect, but a
> highly-privileged credential still exists while connected. Set `GITHUB_OAUTH_SCOPES=public_repo`
> to limit it to public repositories. For an open-source app, the **public-repo path** avoids the
> credential entirely (connect by URL, read anonymously) — the agent receives only a clone URL. The
> narrower, per-repo, read-only path for **private** clone/build is a **GitHub App**: a short-lived
> installation token is minted per claim and handed to the agent for that one deploy (no stored
> token — see above). Connecting GitHub is covered in [sources.md](./sources.md).

### Server access & remote management (SSH)

Deployment never uses SSH — the agent dials **out** and pulls signed jobs (see
[agent.md](./agent.md)). But **dashboard-managed server setup** opens a controlled **inbound**
SSH channel to bootstrap and repair a server: a deliberate, bounded exception to the
no-inbound-SSH default that follows the same discipline as everything else here.

- The **raw bootstrap credential** the user enters is held in memory for the active setup attempt
  only — never stored, never logged, discarded on success **and** failure.
- What persists is a generated **non-root `plorigo` user** with a **dedicated SSH key**, **sealed
  at rest** with the same box / `APP_MASTER_KEY` as secrets, **scoped by an allowlisted sudoers
  drop-in**, **host-key pinned**, and **rotatable / revocable** by the user.
- Every connect, setup step, install, rotation, revocation, and failed auth is **audited**; keys,
  passwords, and tokens are **redacted** everywhere.

The full model — why this isn't the long-lived-root-SSH design the agent trust model bans, plus
the credential lifecycle, least-privilege defaults, and security-review items — is in
[server-management.md](./server-management.md).

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
