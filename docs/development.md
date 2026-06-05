# Development setup

> 🚧 This guide will be fleshed out as the codebase lands. For now it captures the intended
> toolchain and local workflow. See also [CONTRIBUTING.md](../CONTRIBUTING.md).

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

# Run the control plane
go run ./cmd/controlplane

# Run the dashboard (in another terminal)
cd apps/web
pnpm install
pnpm dev
```

## Common tasks (planned)

```bash
go test ./...          # run Go tests
golangci-lint run      # lint Go
buf generate           # regenerate protobuf / ConnectRPC code
pnpm --dir apps/web test
```

If anything here is out of date, please open a docs issue or PR.
