# Development setup

This guide takes you from a fresh clone to a working Plorigo dev environment and describes the
everyday verification loop. The repo is an early-stage modular monolith — a Go control plane, a
Go agent, a Go CLI, and a React/TypeScript dashboard — that leans on generated clients and a
local Postgres, so a couple of commands must run before anything compiles.

> [!NOTE]
> For how the system fits together and where to make a change, see [AGENTS.md](../AGENTS.md)
> and the design docs in [`docs/architecture/`](./architecture/). For planned features and what's
> free vs. paid, see the [roadmap](../ROADMAP.md) — this guide stays focused on building locally.

Everything below is driven by the [`Makefile`](../Makefile); run `make help` to list every target.

## Prerequisites

Install these once. Versions are pinned by the repo where it matters, so you don't have to guess:

- **[Go](https://go.dev/dl/)** — control plane, agent, CLI. Use the version in [`go.mod`](../go.mod).
  The dev tools (`buf`, `sqlc`, `goose`, `golangci-lint`) are pinned there too and run via
  `go tool`, so there's nothing extra to install for them.
- **[Node.js](https://nodejs.org/)** + **[pnpm](https://pnpm.io/)** — the dashboard (`apps/web`).
  The Node version is in [`.nvmrc`](../.nvmrc); pnpm is pinned in `apps/web/package.json` and
  provisioned automatically by Corepack during setup.
- **[Docker](https://docs.docker.com/get-docker/)** — runs the local Postgres, the integration
  tests, and real deployments.
- **[Caddy](https://caddyserver.com/)** — reverse proxy for deployed apps (not needed for the
  basic build/test loop; needed for the deployment e2e).

## First-time setup

From the repo root:

```bash
make setup      # Corepack-provision pnpm, download Go modules, install dashboard deps
make generate   # generate protobuf/ConnectRPC clients (buf) and the SQL layer (sqlc)
```

> [!IMPORTANT]
> **Run `make generate` before you build or test.** The generated Go (`proto/gen/`) and
> TypeScript (`apps/web/src/gen/`) clients are git-ignored, so a fresh checkout won't compile
> until you generate them. Re-run `make generate` whenever you change a `.proto` or a `.sql` file.
> If you forget, `make build`, `make test`, `make lint`, and the dashboard targets stop with that
> same instruction instead of a confusing compiler error.
>
> The sqlc output (`internal/platform/database/db/`) is the exception — it **is** committed. After
> changing a query in `db/query/` or a migration, run `make generate` and commit the regenerated
> files; CI verifies they're in sync and fails the build if they drift.

## The verification loop

These are the same checks CI runs, in the same order, and the ones to run locally before pushing.
None of them need Docker or a database. After `make setup` (once), the quickest path is a single
command:

```bash
make verify   # regenerates, then runs build → test → lint (Go) and the dashboard lint + typecheck
```

To run the steps individually instead, `make generate` first (see above), then:

**Backend (Go + proto)**

| Command | What it does |
|---|---|
| `make build` | compile all Go packages (`go build ./...`) |
| `make test`  | run Go unit tests (`go test ./...`) — excludes integration tests (see below) |
| `make lint`  | `golangci-lint` (incl. module-boundary rules) + `buf lint` for the protos |

**Dashboard (`apps/web`)**

| Command | What it does |
|---|---|
| `make web-check` | lint + typecheck the dashboard (mirrors CI) |
| `make web`       | production build of the dashboard |

For the dashboard build and checks, prefer the `make` targets over calling `pnpm` directly —
they run pnpm from inside `apps/web` so Corepack resolves the pinned pnpm version (running it
from the repo root can pick the wrong major). `make fmt` formats Go code and `make tidy` tidies
`go.mod`.

## Database setup

The control plane, the seed/migrate targets, and the integration tests need a Postgres. Bring
one up with Docker Compose, then apply migrations and create a dev login:

```bash
# Start just Postgres. The controlplane compose service needs APP_MASTER_KEY and would
# collide with `make dev`, so bring up the database on its own.
docker compose -f deploy/docker-compose.yml up -d postgres

make migrate    # apply goose migrations (uses $DATABASE_URL)
make seed       # create a local dev login: dev@plorigo.local / devpassword
```

The compose Postgres listens on `localhost:5432` as `plorigo`/`plorigo`. `make migrate`,
`make seed`, and `make dev` read `DATABASE_URL` and `APP_MASTER_KEY`, which the `Makefile`
defaults to throwaway local values — so these work out of the box. A real deployment sets
`APP_MASTER_KEY` in the environment, which overrides the default. Override the seeded login with
`make seed SEED_EMAIL=you@example.com SEED_PASSWORD=secret123`.

## Running locally

The control plane is secure-by-default (Secure cookies + CSRF on), so use `make dev`, which sets
`PLORIGO_ENV=dev` to let the session cookie work over `http://localhost`. A plain
`go run ./cmd/controlplane` would run in production mode and reject the cookie over http.

```bash
make dev                      # control plane on http://localhost:8080
pnpm --dir apps/web dev       # dashboard (Vite), in a second terminal
```

Sign in with the credentials from `make seed`.

## Import from GitHub (optional)

To exercise GitHub repository import, register a GitHub **OAuth App**
(<https://github.com/settings/applications/new>) and point its **Authorization callback URL** at
the dashboard origin's `/api/github/callback`. In dev that origin is the Vite server, which
proxies `/api/github/*` to the control plane:

```text
Homepage URL:               http://localhost:5173
Authorization callback URL: http://localhost:5173/api/github/callback
```

Then export the credentials and run the dev servers — the callback URL is derived from
`PLORIGO_BASE_URL` (default `http://localhost:5173`), so it matches the App without further config:

```bash
export GITHUB_OAUTH_CLIENT_ID=<client id>
export GITHUB_OAUTH_CLIENT_SECRET=<client secret>
# GITHUB_OAUTH_SCOPES defaults to "repo" (private + public); set "public_repo" to narrow it.
make dev                      # control plane; reads the GITHUB_OAUTH_* vars from the environment
pnpm --dir apps/web dev       # dashboard (Vite), in a second terminal
```

When the variables are unset the dashboard shows the import flow as **not configured**. With them
set, **Projects → Import from GitHub** runs the OAuth handshake, then lets you pick a repository
and branch. The access token is stored encrypted and is never returned by the API — see
[security.md](./architecture/security.md).

## Connect a server

Plorigo deploys to servers you connect by running a small **agent** on them. The dashboard's
**Servers → Connect server** flow creates a server record and shows a **one-line install
command** carrying a single-use registration token:

- In a normal deployment the command is `curl -fsSL <installer> | sudo sh -s -- --control-plane
  <url> --token <token>`, where the installer is [`scripts/install-agent.sh`](../scripts/install-agent.sh):
  on a fresh Ubuntu 22.04/24.04 LTS box it installs and verifies the prerequisites the agent needs
  (Docker Engine with BuildKit, the Caddy binary), creates the data directory, downloads the agent
  binary from GitHub Releases and **checksum-verifies** it, and runs it as a systemd service.
- In dev mode (`make dev`) the command instead runs the agent straight from your checkout —
  `go run ./cmd/agent --control-plane http://localhost:8080 --token <token>` — so you exercise
  your working copy, not a published binary. It also points the agent at a Plorigo-managed
  Caddyfile under `.context/` and uses non-privileged local Caddy ports derived from the
  control-plane port.

On first start the agent generates an ed25519 keypair, exchanges the one-time token for a durable
credential via `agent.v1.AgentService/Register`, and writes both to its data dir (`--data-dir`,
mode `0600`). It then heartbeats over an **outbound** connection, so the server card flips to
**online** within a few seconds. Restart the agent and it **resumes** from that stored identity —
no token needed; the token is only for the first registration, is single-use, and expires after an
hour. Re-running the install command mints a fresh token and rotates the credential, so mint a new
one from the server card if you ever need it again. See
[docs/architecture/agent.md](./architecture/agent.md) for the trust model.

The installer prepares a fresh **Ubuntu 22.04 / 24.04 LTS** server end-to-end: it rejects other
OSes with a clear reason, installs Docker Engine (from Docker's official apt repo) and the Caddy
binary (the agent runs Caddy itself, so the packaged `caddy.service` is disabled), and verifies
Docker daemon access, that ports 80/443 are free, and outbound control-plane connectivity before
reporting success. It is **idempotent** — re-run it to reconnect, repair, or rotate the token (a
re-run rewrites the unit so the fresh token takes effect). It must run as **root** (hence `sudo`),
never prints the token, and emits a plain-English error with a recovery hint for the known failure
modes (held apt lock, missing root, unsupported OS, Docker install failure, occupied ports,
unreachable control plane, agent startup failure).

The agent binary itself is downloaded over HTTPS from the project's GitHub Releases (the latest
release, or `PLORIGO_AGENT_VERSION=<tag>`) and its **sha256 is verified against the release's
`checksums.txt` before it is run as root** — a mismatch or missing checksum aborts the install
(`PLORIGO_INSTALL_SKIP_VERIFY=1` bypasses it, only for local/offline testing). Release binaries
also carry SLSA build-provenance attestations, so anyone can independently confirm where a binary
came from: `gh attestation verify plorigo-agent-linux-amd64 --repo Plorigo/plorigo`.

> [!NOTE]
> Pass `--skip-prep` to install only the agent + service and skip host preparation — for technical
> users whose box already has Docker and Caddy, or environments without systemd (it then runs the
> agent in the foreground). The **dashboard-managed** SSH setup path — which drives this same
> installer over SSH and creates a non-root management user — is a later step; see
> [server-management.md](./architecture/server-management.md) for the model and
> [ROADMAP.md](../ROADMAP.md) for sequencing.

> [!TIP]
> If a local deployment fails with `Caddy CLI was not found in PATH`, install Caddy first
> (`brew install caddy` on macOS), then mint and run a fresh dev install command from the
> dashboard. The agent validates the generated route config and can start Caddy automatically
> if no local Caddy instance is already running.

### Verifying the install flow end-to-end

The installer is covered at three levels:

- **Installer logic (in CI).** `internal/app/install_agent_shim_test.go` runs the real
  `scripts/install-agent.sh` with fake `apt-get`/`docker`/`caddy`/`systemctl`/`curl`/`ss` on `PATH`
  and a fake `/etc/os-release`, so every branch — OS gating, idempotent re-runs, the named failure
  modes, port handling, connectivity, and token redaction — is exercised on both Ubuntu 22.04 and
  24.04 without Docker or root. It runs as part of `make test`.
- **Register + resume on a real container.** `make e2e-agent` builds a Linux agent binary, boots an
  in-process control plane, and runs the real installer + agent in a clean `ubuntu:24.04` container
  (with `--skip-prep`, since the container has no systemd/Docker) — asserting the server comes
  **online** and that after a restart the agent **resumes** the same identity instead of
  re-registering. It needs Docker and a migrated Postgres and is **not** part of `make test` or CI;
  run it locally before changing the agent or installer.
- **Bare-server preparation (manual).** To verify the full host preparation, run the actual one-line
  command on a fresh **Ubuntu 22.04** and **24.04** server (a real VPS, or `multipass launch 22.04`/
  `24.04`, or a privileged systemd container) against a reachable control plane. Confirm Docker and
  the `caddy` binary are installed, the packaged `caddy.service` is disabled, `systemctl is-active
  plorigo-agent` is `active`, the server flips **online** in the dashboard, and a **re-run is
  idempotent** (rotates the token, stays online). Record the exact images/commands in the PR.

### Fresh-VPS → first-app release gate

The full release gate — a bare VPS reaching a **reachable app** through *both* the manual and the
dashboard-managed SSH paths — is the manual procedure in
[docs/verification/fresh-vps-e2e.md](./verification/fresh-vps-e2e.md), driven by
`make e2e-fresh-vps` (`scripts/e2e-fresh-vps.sh`). It needs two real VPSes and a publicly-reachable
control plane, so it is **not** in CI; only its driver logic and secret redaction are
(`internal/app/e2e_fresh_vps_shim_test.go`, in `make test`). Run `E2E_DRYRUN=1 make e2e-fresh-vps`
to print the plan, then record the run in the gating PR.

### Releasing the agent

Prebuilt agent binaries are published as **GitHub Release assets** so the one-line installer can
fetch them on a server with no Go toolchain. Cutting a release is a manual, deliberate step:

1. Create (and publish) a GitHub Release with a version tag, e.g. `v0.1.0`.
2. Publishing it triggers [`.github/workflows/release-agent.yml`](../.github/workflows/release-agent.yml),
   which builds `plorigo-agent-linux-amd64` and `plorigo-agent-linux-arm64` (version embedded via
   `-ldflags`), writes `checksums.txt`, attaches all three to the release, and records a SLSA
   build-provenance attestation for each binary.
3. To (re)build assets for an existing tag without re-publishing, run the workflow via
   **Actions → Release agent → Run workflow** and pass the tag (`--clobber` overwrites cleanly).

The workflow runs with least privilege (read-only by default; the job adds only
`contents:write` + `id-token:write` + `attestations:write`), pins every action by commit SHA, and
checks out with `persist-credentials: false`. The installer verifies the checksum of what it
downloads; consumers can additionally verify provenance with `gh attestation verify`.

## Integration tests

Integration tests sit behind a build tag and run against a real Postgres, so `make test`
**excludes** them. With Postgres up and migrated (see [Database setup](#database-setup)):

```bash
go test -tags integration ./internal/app/...
```

They read `DATABASE_URL` and `APP_MASTER_KEY` from the environment; the `Makefile` defaults are
the same throwaway values CI uses, so exporting those (or running with the Makefile's
environment) is enough.

## Conductor workspaces (optional)

Everything above is all you need. If you develop with [Conductor](https://conductor.build), each
workspace runs an **isolated** copy of the dev stack so multiple workspaces work in parallel
without colliding. The lifecycle is driven by the `scripts/conductor-*.sh` scripts:

- **Setup** (`scripts/conductor-setup.sh`) runs `make setup` + `make generate`.
- **Run** (`scripts/conductor-run.sh`) brings up this workspace's own Postgres, applies
  migrations, and starts the control plane + Vite dashboard against it.
- **Archive** (`scripts/conductor-archive.sh`) tears the stack down and frees disk.

Each workspace gets its own Docker Compose project (`plorigo-<workspace>`), database, volume,
network, and host ports — all derived once in `scripts/conductor-env.sh`:

| Resource | Host port |
|---|---|
| Control plane (API) | `CONDUCTOR_PORT` |
| Dashboard (Vite) | `CONDUCTOR_PORT + 1` |
| Postgres | `CONDUCTOR_PORT + 2` |

> [!WARNING]
> **Archiving a workspace is destructive to its local data.** The archive script runs
> `docker compose … down -v` (removing the Postgres volume) and deletes regenerable,
> git-ignored artifacts (`node_modules`, `proto/gen`, `apps/web/dist`, build output, …) to
> reclaim disk. Committed code (sqlc output, migrations, `.proto` sources) is never touched —
> everything removed is regenerable with `make setup && make generate`.

The non-Conductor paths above (`make dev`, a manual `docker compose up`) use the shared default
project on Postgres port **5432**.

**One-time legacy cleanup.** If you used Plorigo with Conductor before per-workspace isolation
existed, an old shared `plorigo` project and its volume linger on port 5432. Remove them once
(this deletes that old local database — make sure no workspace still needs it):

```bash
APP_MASTER_KEY=x docker compose -p plorigo -f deploy/docker-compose.yml \
  --profile storage down -v --remove-orphans
```

---

If anything here is out of date, please open a docs issue or PR.
