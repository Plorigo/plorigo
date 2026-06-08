# Development setup

> 🚧 This guide will be fleshed out as the codebase lands. For now it captures the intended
> toolchain and local workflow. See also [CONTRIBUTING.md](../CONTRIBUTING.md).

> [!NOTE]
> For how the system fits together and where to make a change, see [AGENTS.md](../AGENTS.md)
> and the design docs in [`docs/architecture/`](./architecture/).

## Prerequisites

- [Go](https://go.dev/dl/) (latest stable) — control plane, agent, CLI
- [Node.js](https://nodejs.org/) (LTS) + [pnpm](https://pnpm.io/) — dashboard (`apps/web`)
- [Docker](https://docs.docker.com/get-docker/) — required to run and test deployments
- [buf](https://buf.build/) — generate ConnectRPC / protobuf code
- [sqlc](https://sqlc.dev/) — type-safe SQL
- [goose](https://github.com/pressly/goose) — database migrations
- [golangci-lint](https://golangci-lint.run/) — Go linting
- [Caddy](https://caddyserver.com/) — reverse proxy / SSL

## Quick start (planned)

```bash
git clone git@github.com:<you>/plorigo.git
cd plorigo

# Start local dependencies (Postgres, etc.)
docker compose -f deploy/docker-compose.yml up -d

# Run the control plane in dev mode. The control plane is secure-by-default
# (Secure cookies + CSRF on), so `make dev` sets PLORIGO_ENV=dev to let the session
# cookie work over http://localhost. A plain `go run ./cmd/controlplane` would run in
# production mode and reject the cookie over http.
make dev

# Run the dashboard (in another terminal)
cd apps/web
pnpm install
pnpm dev
```

## Conductor workspaces

When you develop with [Conductor](https://conductor.build), each workspace runs an
**isolated** copy of the dev stack so multiple workspaces work in parallel without
colliding. The lifecycle is driven by [`conductor.json`](../conductor.json) and the
`scripts/conductor-*.sh` scripts:

- **Setup** (`scripts/conductor-setup.sh`) installs the toolchain and generates code.
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

The non-Conductor paths above (`scripts/dev.sh`, `make dev`, a manual `docker compose up`)
still use the shared default project on Postgres port **5432**.

**One-time legacy cleanup.** If you used Plorigo with Conductor before per-workspace
isolation existed, an old shared `plorigo` project and its volume linger on port 5432. Remove
them once (this deletes that old local database — make sure no workspace still needs it):

```bash
APP_MASTER_KEY=x docker compose -p plorigo -f deploy/docker-compose.yml \
  --profile storage down -v --remove-orphans
```

## Common tasks (planned)

```bash
go test ./...          # run Go tests
golangci-lint run      # lint Go
buf generate           # regenerate protobuf / ConnectRPC code
pnpm --dir apps/web test
```

If anything here is out of date, please open a docs issue or PR.
