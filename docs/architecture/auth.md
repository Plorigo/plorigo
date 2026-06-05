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
  revokes **all** of the user's sessions.
- **API tokens** (`api_tokens`) authenticate the CLI/agent. Format `plk_` + 32 random
  bytes; shown **once** at creation; stored as `sha256` plus a display prefix.
- **Email verification / password reset** use single-use `user_tokens` (hashed,
  expiring). `RequestPasswordReset` always returns OK (no account enumeration).

## The session cookie

`plorigo_session`: `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` in production
(off for `http://localhost` in dev, derived from `PLORIGO_ENV`). The `auth` handler
sets it on register/login and clears it on logout; everything else is stateless.

## The interceptor (request → principal)

A single ConnectRPC interceptor (`internal/app`) wraps every service. It:

1. resolves a `principal.Principal` from the `Authorization: Bearer` header
   (CLI/agent) or the session cookie (browser), via the `auth` resolvers;
2. applies a **CSRF** guard to cookie-authenticated requests (see below);
3. injects the principal into the context;
4. rejects non-public procedures that have no principal with `Unauthenticated`.

A small allowlist of **public** procedures (`Register`, `Login`,
`RequestPasswordReset`, `ResetPassword`, `VerifyEmail`) bypasses step 4. The
interceptor never decides *what* a caller may do — that is `policy.Authorize`,
called by each privileged service before it mutates.

### CSRF

Only ambient (cookie) authority needs CSRF protection. For cookie-authenticated
requests the interceptor requires the `Connect-Protocol-Version` header (which a
cross-site HTML form cannot set without a CORS preflight) and rejects a cross-site
`Sec-Fetch-Site`, on top of `SameSite=Lax`. Bearer requests carry no cookie and are
exempt; never honor a cookie and a bearer token on the same request. (Enforced in
production; relaxed in dev, where the Vite proxy makes origins awkward.)

## Authorization (the policy model)

`policy.Authorize(ctx, principal, action, resource)` looks up the caller's role for
`resource.WorkspaceID` (through the `membership` reader) and consults a role →
permission matrix. Roles, highest first: **owner → admin → member → viewer** (in
`authz`). A missing membership denies. Privileged services call it **before** the
`WithinTx` block; the audit row commits **with** the action (see
[modules.md](./modules.md), Rule 3). Finer, target-dependent rules (e.g. an owner
cannot be removed or demoted) live in the owning service, not the matrix.

## Registration & bootstrap

The first registration on a fresh install becomes the owner of a new workspace;
**every** registration creates a personal workspace the user owns, so a user always
lands somewhere. This runs in one transaction via
`projects.CreateInitialWorkspace`, which deliberately **skips** `policy.Authorize`
(the act of becoming the owner of a brand-new workspace) and audits the creation.
`PLORIGO_ALLOW_OPEN_REGISTRATION=false` restricts new sign-ups to the bootstrap user
and (later) invited users.

## Email

`internal/platform/mailer` sends via SMTP when `SMTP_HOST` is set; otherwise it
**logs** the message — including the verify/reset link — so a self-hoster without a
mail server can still complete the flow. That log line is the only place a link
appears; raw tokens never reach the audit trail or normal logs.

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
