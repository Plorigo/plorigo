# Authentication & authorization

> [!NOTE]
> **Status: target architecture (design contract).** This describes the *intended*
> design; write code to match it and update this doc in the same change when the
> design evolves. For scope and sequencing see [ROADMAP.md](../../ROADMAP.md).

This is the authorization seam — the first slice after the scaffold and the
prerequisite for every privileged module (see [modules.md](./modules.md), Rule 4).
Read it before touching identity, sessions, tokens, or the policy model. The
governing rules live in [security.md](./security.md); this is how they are realized.

> The line, stated plainly: **who you are** is `auth`; **what you may do** is `policy`.

## Components

| Concern | Where |
|---|---|
| Identity: users, sessions, API tokens, email-verify / password-reset | `internal/auth` (full module) |
| Authorization decisions (role → permission) | `internal/policy` (decision-only; no tables) |
| Role lookup for a workspace | `internal/membership` (provider-only read module) |
| Workspace / membership **writes** + bootstrap | `internal/projects` (owns the workspace aggregate) |
| The neutral seams every module shares | `internal/platform/{principal,authz,passwd}` |
| Request → principal, CSRF, public allowlist | `internal/app/auth_interceptor.go` |
| Email delivery (log / SMTP) | `internal/platform/mailer` |

The cross-module graph is acyclic and import-free: `auth → projects → policy →
membership`, with everyone depending only on the neutral `platform` packages.
`authz.Authorizer` (implemented by `policy`) and `principal.Principal` (the ctx
identity) are the seams that make this work without any module importing another.

## Identity

- **Passwords** are hashed with **argon2id** (`internal/platform/passwd`), stored as a
  PHC string in `users.password_hash` so the cost can evolve without a migration.
- **Sessions** are DB-backed (`sessions`). The browser holds an opaque 256-bit token
  in a cookie; only its `sha256` is stored. Login issues a fresh token (so a
  pre-login cookie can't fixate a session); logout revokes that row; a password reset
  revokes **all** of the user's sessions. Resolving a session (or API token) is a
  single `UPDATE … RETURNING` that also refreshes `last_used_at`, so it stays live for
  display and a future idle-expiry; expiry today is absolute (30 days).
- **API tokens** (`api_tokens`) authenticate the CLI/agent. Format `plk_` + 32 random
  bytes; shown **once** at creation; stored as `sha256` plus a display prefix.
- **Email verification / password reset** use single-use `user_tokens` (hashed,
  expiring). `RequestPasswordReset` always returns OK (no account enumeration).

## The session cookie

`plorigo_session`: `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` in production.
**`PLORIGO_ENV` is secure-by-default**: the app is in dev mode (non-`Secure` cookie,
relaxed CSRF) ONLY when it is explicitly `dev`/`development`/`local`; unset or anything
else means production, so a deploy that forgets the var still gets `Secure` + CSRF, not
the reverse. The `auth` handler sets the cookie on **login** (registration never
auto-logs-in — see below) and clears it on logout; everything else is stateless.

## The interceptor (request → principal)

A single ConnectRPC interceptor (`internal/app`) wraps every service. It:

1. resolves a `principal.Principal` from the `Authorization: Bearer` header
   (CLI/agent) or the session cookie (browser), via the `auth` resolvers;
2. applies a **CSRF** guard (cross-site requests are rejected in production — see below);
3. injects the principal into the context;
4. rejects non-public procedures that have no principal with `Unauthenticated`.

A small allowlist of **public** procedures (`Register`, `Login`,
`RequestPasswordReset`, `ResetPassword`, `VerifyEmail`) bypasses step 4. The
interceptor never decides *what* a caller may do — that is `policy.Authorize`,
called by each privileged service before it mutates.

### CSRF

In production the interceptor rejects a cross-site `Sec-Fetch-Site` for **every**
procedure — including the public `Login`/`Register`, so a forged cross-site POST can't
log a victim in or act on them. Cookie-authenticated requests additionally require the
`Connect-Protocol-Version` header (which a cross-site HTML form cannot set without a
CORS preflight), on top of `SameSite=Lax`. Bearer (CLI/agent) requests send neither
`Sec-Fetch-Site` nor a cookie, so they are unaffected; never honor a cookie and a
bearer token on the same request. (Relaxed in dev, where the Vite proxy makes origins
awkward.)

## Authorization (the policy model)

`policy.Authorize(ctx, principal, action, resource)` looks up the caller's role for
`resource.WorkspaceID` (through the `membership` reader) and consults a role →
permission matrix. Roles, highest first: **owner → admin → member → viewer** (in
`authz`). A missing membership denies. Privileged services call it **before** the
`WithinTx` block; the audit row commits **with** the action (see
[modules.md](./modules.md), Rule 3). Finer, target-dependent rules (e.g. an owner
cannot be removed or demoted) live in the owning service, not the matrix.

> [!NOTE]
> In this slice ownership only **accumulates**: an owner can't be demoted, removed, or
> transferred, so there is no recovery path for a mis-assigned owner. A safe
> ownership-transfer flow is a roadmap item — see [ROADMAP.md](../../ROADMAP.md).

## Registration & bootstrap

**Registration never auto-logs-in.** A brand-new signup and an attempt on an
already-registered email return the *same* generic response (no user, no session, no
cookie) — anti-enumeration — and the address owner gets either a verification link or a
"you already have an account" email. The user logs in afterwards. Every new account
gets a personal workspace it owns (so a user always lands somewhere), created in one
transaction via `projects.CreateInitialWorkspace`, which deliberately **skips**
`policy.Authorize` (you are becoming the owner of a brand-new workspace) and audits the
creation.

`PLORIGO_ALLOW_OPEN_REGISTRATION=false` restricts new sign-ups to the bootstrap (first)
user. The check runs **inside** the registration transaction behind a
`pg_advisory_xact_lock`, so two concurrent first-registrations can't both win the
bootstrap.

`PLORIGO_REQUIRE_EMAIL_VERIFICATION=true` **enforces** verification: `Login` is rejected
with `PermissionDenied` until the address is verified (checked *after* the password, so
it can't be used to enumerate accounts). With it off (the default) an account is usable
via login immediately after registration.

## Email

`internal/platform/mailer` sends via SMTP when `SMTP_HOST` is set; otherwise it
**logs** the message — including the verify/reset link — so a self-hoster without a
mail server can still complete the flow. That log line is the only place a link
appears; raw tokens never reach the audit trail or normal logs.

Caveats: the SMTP mailer relies on **STARTTLS** negotiation (no implicit-TLS / port-465
mode yet — use a STARTTLS-capable relay). Verify/reset links carry the token in the URL
**query string** (standard for email links); the dashboard sets `Referrer-Policy:
no-referrer` so the token isn't leaked via the `Referer` header, though it can still
land in browser history — links are single-use and short-lived to bound the window.

## Surfaces

- **Dashboard** (`apps/web`): a route guard redirects to `/login` when
  unauthenticated; the transport sends the cookie (`credentials: "include"`); see
  [dashboard.md](./dashboard.md).
- **CLI**: `plorigo login` exchanges email/password for an API token stored under the
  user's config dir (`0600`) and attached as a bearer token on every command.

## Configuration

`APP_MASTER_KEY` and `DATABASE_URL` stay required; the rest have defaults:
`PLORIGO_ENV`, `PLORIGO_BASE_URL` (email links), `PLORIGO_ALLOW_OPEN_REGISTRATION`,
`PLORIGO_REQUIRE_EMAIL_VERIFICATION`, and `SMTP_*` / `EMAIL_FROM`. See
[`.env.example`](../../.env.example).
