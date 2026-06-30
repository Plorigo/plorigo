# Sources — integrations & connecting Git providers

Plorigo deploys from a Git repository. How it reaches that repository is the **source**. A workspace
connects **integrations** (provider connections) and a service records which connection it builds
from (`services.connection_id`) plus how the source is reached (`services.source_access`):

| `source_access` | How it's reached | Built & deployed? | Connection |
|---|---|---|---|
| `public` | Anonymous clone of a public repo (clone URL only) | **Yes** | None |
| `app` | A GitHub **App** installation — short-lived per-use token | **Yes** (private repos) | A specific App connection |
| `oauth` | A GitHub **OAuth** account token (discovery/listing) | No (discovery only) | A specific OAuth connection |

A workspace may have **many** integrations — several GitHub App installations (one per org) and/or
OAuth accounts, and (later) other providers. Each is a row in `source_connections`
(`provider` + `kind` + account identity); the deploy path mints the credential for the **service's
own** connection, not "the workspace's connection". This doc is the operator + contributor
walkthrough; the credential model lives in [security.md](./security.md), the preview lifecycle in
[deployment-engine.md](./deployment-engine.md).

> Provider **server config** (OAuth client id/secret; the App) is read per provider; the per-workspace
> connections are data. The dashboard's **Integrations** page lists connections, offers "Add
> integration" (per provider, with unconfigured/unimplemented providers shown disabled), and
> disconnects. The public-repo path (paste a URL) always works without any integration.

## Provider seam (extending to GitLab, etc.)

All VCS access goes through a **`Provider` interface + registry** in `internal/sources` (the GitHub
adapter wraps `internal/platform/github`). The sources service — and through it services,
deployments, and webhooks — is **provider-agnostic**: adding GitLab/Bitbucket means implementing
`Provider` and registering it (plus its own `/api/<provider>/*` connect routes), not touching the
other modules. GitHub is the only provider implemented today.

## The two GitHub paths, and when to use each

- **GitHub OAuth App** — connect a GitHub *account*; Plorigo lists its repositories for the picker.
  The OAuth scope defaults to `repo` (read/write to **all** the user's repos), so the token is broad
  and long-lived and is **never sent to the agent** — an OAuth connection is **discovery only, not
  built**. Set `GITHUB_OAUTH_SCOPES=public_repo` to narrow it.

- **GitHub App** — install an *App* (on a user or org) and grant it specific repositories. Plorigo
  stores only the installation id and mints a **short-lived installation token** per use. This is the
  **private-repo build + deploy** path and the basis for **webhook-driven PR previews**. A workspace
  can install it on several orgs — each is its own connection.

## Operator setup

### 1. GitHub OAuth App (optional — repo discovery)

Create an OAuth App at GitHub → Settings → Developer settings → **OAuth Apps** → New. Set the
**Authorization callback URL** to `{PLORIGO_BASE_URL}/api/github/callback`. Then set:

```
GITHUB_OAUTH_CLIENT_ID=...
GITHUB_OAUTH_CLIENT_SECRET=...
GITHUB_OAUTH_SCOPES=repo          # or public_repo to limit the grant
```

### 2. GitHub App (recommended — private deploys + PR previews)

There are two ways to set this up. **Automated registration is the easy path** and needs no env vars.

#### Automated registration (recommended)

From the dashboard's **Integrations** page, a **workspace owner** clicks **Register GitHub App**
(optionally naming a GitHub organization). This uses GitHub's **App-manifest flow**: Plorigo posts a
manifest (the permissions, webhook URL, and events below, all preset) to GitHub; the operator
confirms and names the app; GitHub redirects back with a one-time code that the control plane
exchanges for the app's id, slug, **private key**, and **webhook secret**. Those are **sealed at
rest** (AES-256-GCM, `APP_MASTER_KEY`) in a singleton `github_app_config` row — write-only, never
returned by an RPC, never logged. No restart is needed; the app is live immediately. Re-registering
replaces it. This is owner-only (`github_app.register`) because the app is shared by every workspace.

> The manifest flow is **disabled when the env vars below are set** (env takes precedence). To
> register from the dashboard instead, leave `GITHUB_APP_*` unset.

#### Manual registration (env vars)

Alternatively, create a GitHub App by hand at GitHub → Settings → Developer settings →
**GitHub Apps** → New:

- **Repository permissions:** **Contents** → Read-only, **Metadata** → Read-only,
  **Pull requests** → Read-only.
- **Webhook:** URL `{PLORIGO_BASE_URL}/api/github/webhook`, **Secret** = a random string (this is
  `GITHUB_WEBHOOK_SECRET`). Subscribe to the **Pull request** event.
- **Private key:** generate one and download the PEM.

Then set the server config:

```
GITHUB_APP_ID=...                 # the App's numeric id
GITHUB_APP_PRIVATE_KEY=...        # the PEM contents (single line with \n escapes, or a file/multi-line value)
GITHUB_APP_SLUG=...               # the App's URL slug — builds https://github.com/apps/<slug>/installations/new
GITHUB_WEBHOOK_SECRET=...         # must match the webhook secret above
```

`PLORIGO_PREVIEW_TTL_HOURS` (default `72`, `0` disables) controls when idle PR previews are swept
and torn down.

All of these are in [`.env.example`](../../.env.example) and surfaced in
[`deploy/docker-compose.yml`](../../deploy/docker-compose.yml).

## Per-workspace connection (dashboard)

The **Integrations** page (workspace nav → *Integrations*) is the home for connecting GitHub:

- **GitHub App** → *Install GitHub App* redirects to GitHub's install page, where the user picks the
  repositories to grant. The setup callback (`/api/github/app/setup`) ties the new installation to
  the workspace (one App connection per workspace) and records it (audited). Re-running *Manage
  repositories* reopens GitHub's install settings to add/remove repos.
- **GitHub account (OAuth)** → *Connect GitHub* runs the OAuth handshake; *Disconnect* revokes the
  token at GitHub (disconnect is blocked while services still reference the connection).

The same install/connect actions are also embedded in the add-service flow (*Import Git
Repository*), so a user can connect inline while creating a service.

## How a private repo is built

1. A service backed by the App has `source_access = 'app'` and is **buildable**. The dashboard
   resolves a private repo through the workspace's installation when creating the service.
2. When the service's server agent **claims** a deploy (or webhook preview), the control plane mints
   a fresh installation token (`sources.Service().InstallationToken(workspaceID)`) and includes it
   in the claim as `git_credential`.
3. The agent clones with that token applied as the password of an `x-access-token` HTTP basic-auth —
   it is **not** embedded in the clone URL, so it never lands in a log line or the stored URL.
4. The token is short-lived (GitHub expires it within the hour) and is **never persisted, returned
   by an RPC, or logged**. If no installation is connected when a private deploy is claimed, the
   deployment is failed with a plain-English message rather than handing the agent an anonymous
   clone that would 404.

See [security.md](./security.md) for the full credential model and
[deployment-engine.md](./deployment-engine.md) for the preview lifecycle.
