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
  tests, and (once it lands) real deployments.
- **[Caddy](https://caddyserver.com/)** — reverse proxy / SSL for the deployment path (not needed
  for the basic build/test loop).

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

## Connect a server

Plorigo deploys to servers you connect by running a small **agent** on them. The dashboard's
**Servers → Connect server** flow creates a server record and shows a **one-line install
command** carrying a single-use registration token:

- In a normal deployment the command is `curl -fsSL <installer> | sh -s -- --control-plane <url>
  --token <token>`, where the installer is [`scripts/install-agent.sh`](../scripts/install-agent.sh):
  it puts the agent binary in place and runs it as a systemd service.
- In dev mode (`make dev`) the command instead runs the agent straight from your checkout —
  `go run ./cmd/agent --control-plane http://localhost:8080 --token <token>` — so you exercise
  your working copy, not a published binary.

On first start the agent generates an ed25519 keypair, exchanges the one-time token for a durable
credential via `agent.v1.AgentService/Register`, and writes both to its data dir (`--data-dir`,
mode `0600`). It then heartbeats over an **outbound** connection, so the server card flips to
**online** within a few seconds. Restart the agent and it **resumes** from that stored identity —
no token needed; the token is only for the first registration, is single-use, and expires after an
hour. Re-running the install command mints a fresh token and rotates the credential, so mint a new
one from the server card if you ever need it again. See
[docs/architecture/agent.md](./architecture/agent.md) for the trust model.

> [!NOTE]
> Today the installer assumes a Linux host that already has Docker, run as root with systemd
> (otherwise it runs the agent in the foreground). Preparing a *bare* server — installing
> Docker/Caddy, OS checks, idempotent re-runs — is a later step; see [ROADMAP.md](../ROADMAP.md).

### Verifying the install flow end-to-end

`make e2e-agent` exercises the whole loop against a **real server-like environment**. With Docker
running and a migrated Postgres up (see [Database setup](#database-setup)), it builds a Linux agent
binary, boots an in-process control plane, then runs the real installer and agent in a clean
`ubuntu:24.04` container — asserting the server comes **online**, and that after a restart the agent
**resumes** the same identity instead of re-registering. It needs Docker and is **not** part of
`make test` or CI, so run it locally before changing the agent or installer.

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
