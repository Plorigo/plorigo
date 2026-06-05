# Contributing to Plorigo

First off — thank you for taking the time to contribute! 🎉 Plorigo is built in the open and
we welcome contributions of every size: bug reports, docs, tests, and code.

Because Plorigo manages **servers, secrets, databases, and production deployments**, we hold
contributions to a high bar for **correctness and safety**. This guide explains how to get set
up and what to expect from the review process.

> [!NOTE]
> Plorigo is in early development. If you're planning a non-trivial change, please
> **open an issue or a [Discussion](https://github.com/Plorigo/plorigo/discussions) first**
> so we can align on the approach before you invest a lot of time.

## Table of contents

- [Code of Conduct](#code-of-conduct)
- [Ways to contribute](#ways-to-contribute)
- [Development setup](#development-setup)
- [Project structure](#project-structure)
- [Branching & commits](#branching--commits)
- [Coding standards](#coding-standards)
- [Testing](#testing)
- [Opening a pull request](#opening-a-pull-request)
- [PR review & safety process](#pr-review--safety-process)
- [Contributor License Agreement (CLA)](#contributor-license-agreement-cla)
- [Use of AI tools](#use-of-ai-tools)
- [License](#license)

## Code of Conduct

This project follows the [Contributor Covenant](./CODE_OF_CONDUCT.md). By participating you
agree to uphold it. Report unacceptable behavior to **i.babirli@outlook.com**.

## Ways to contribute

- **Report bugs** — use the [bug report form](https://github.com/Plorigo/plorigo/issues/new/choose). Include version, deployment method, OS, and logs.
- **Suggest features** — open a [feature request](https://github.com/Plorigo/plorigo/issues/new/choose) or start a Discussion.
- **Improve docs** — typos to whole guides; docs PRs are always welcome.
- **Write code** — look for issues labeled [`good first issue`](https://github.com/Plorigo/plorigo/labels/good%20first%20issue) and [`help wanted`](https://github.com/Plorigo/plorigo/labels/help%20wanted).

## Development setup

> 🚧 As the codebase lands, exact commands will be documented in `docs/development.md`.
> The toolchain the project is built on:

**Backend / agent / CLI (Go)**

- [Go](https://go.dev/dl/) (latest stable)
- [Docker](https://docs.docker.com/get-docker/) (Docker Engine — required to run and test deployments)
- [buf](https://buf.build/docs/installation) — generate ConnectRPC / protobuf code
- [sqlc](https://docs.sqlc.dev/en/latest/overview/install.html) — type-safe SQL
- [goose](https://github.com/pressly/goose) — database migrations
- [golangci-lint](https://golangci-lint.run/) — linting

**Dashboard (web)**

- [Node.js](https://nodejs.org/) (LTS) and [pnpm](https://pnpm.io/installation)

**Infrastructure for local dev**

- PostgreSQL (via Docker Compose) and [Caddy](https://caddyserver.com/) for proxy/SSL.

A typical loop will look like:

```bash
# clone your fork
git clone git@github.com:<you>/plorigo.git && cd plorigo

# bring up local dependencies (Postgres, etc.)
docker compose -f deploy/docker-compose.yml up -d   # (coming soon)

# backend
go run ./cmd/controlplane

# dashboard
cd apps/web && pnpm install && pnpm dev
```

## Project structure

See the [repository structure](./README.md#repository-structure) in the README. In short:
`cmd/` holds the Go binaries (control plane, agent, CLI), `internal/` the modules
(auth, deployments, secrets, backups, …), `apps/web/` the dashboard, and `proto/` the API
contracts. Changes that touch a module are routed to its owners via
[CODEOWNERS](./.github/CODEOWNERS).

Before working on a subsystem, read its design doc. [AGENTS.md](./AGENTS.md) has a
**documentation map** that routes you to the right doc in [`docs/architecture/`](./docs/architecture/),
and [`docs/conventions.md`](./docs/conventions.md) covers formatting, generated code, and testing.

## Branching & commits

- Branch off `main`. Use a descriptive branch name, e.g. `fix/agent-reconnect` or `feat/preview-urls`.
- `main` is the integration branch and is **protected** — all changes land via pull request.
- We use **[Conventional Commits](https://www.conventionalcommits.org/)** for commit messages
  and PR titles (PRs are squash-merged, so the PR title becomes the commit):
  - `feat: add per-branch preview URLs`
  - `fix(agent): reconnect after control-plane restart`
  - `docs: clarify backup retention`
  - Other common types: `chore`, `refactor`, `test`, `perf`, `ci`, `build`.

## Coding standards

- **Go**: code must be `gofmt`/`goimports` clean and pass `golangci-lint`. Prefer small,
  well-tested packages. Handle errors explicitly; never ignore an error from a privileged
  operation (Docker, Caddy, filesystem, secrets).
- **TypeScript/React**: follow the project ESLint/Prettier config; keep components typed and
  accessible.
- **Generated code** (protobuf, sqlc) is produced by tooling — don't hand-edit it; regenerate.
- **No secrets in code or logs.** Secret scanning + push protection are enabled on this repo.

## Testing

- Add or update tests for the behavior you change. Bug fixes should come with a regression test.
- Run the relevant suites locally before pushing (`go test ./...`, `pnpm test`).
- For changes to the **deployment, agent, secrets, or backup** paths, describe how you tested
  against a real Docker environment in your PR — these paths affect users' production systems.

## Opening a pull request

1. Make sure your branch is up to date with `main`.
2. Fill in the [pull request template](./.github/PULL_REQUEST_TEMPLATE.md) completely — link the
   issue, describe what you tested, and complete the checklist.
3. Keep PRs focused; smaller PRs are reviewed faster.
4. CI must be green and conversations resolved before merge.

## PR review & safety process

Every PR is reviewed before it can be merged. Concretely:

- ✅ **Required review** — at least one maintainer / code owner approves (see [CODEOWNERS](./.github/CODEOWNERS)).
- ✅ **Automated checks** — CodeQL code scanning and dependency review run on PRs; CI (build/lint/test) is added as the codebase grows. Secret-scanning push protection blocks committed credentials.
- ✅ **Safety lens** — reviewers pay special attention to anything touching privileged operations (Docker, host commands, secrets, backups, the agent, and the AI/MCP gateway). Changes that broaden what an AI agent or an unprivileged user can do get extra scrutiny.
- ✅ **Conversations resolved** and a clean, linear history (squash merge).

Don't be discouraged by review feedback — it's how we keep a tool that runs on people's
servers trustworthy.

## Contributor License Agreement (CLA)

To keep Plorigo sustainable as an open-core project, contributors are asked to sign a
**Contributor License Agreement** the first time they open a PR. A bot will comment with a link;
signing is a one-time, one-click step. The CLA lets the project offer commercial/enterprise
licenses alongside the AGPL core — it does **not** take away your rights to your own work. See
[CLA.md](./CLA.md) for the full text.

## Use of AI tools

AI-assisted contributions are welcome, but **you must disclose them** (there's a checkbox in
the PR template) and you are responsible for understanding and testing every line you submit.
Please don't open PRs you can't explain. Maintainers may ask for additional tests or context.

## License

By contributing, you agree that your contributions will be licensed under the project's
[AGPL-3.0 license](./LICENSE) and the terms of the [CLA](./CLA.md).
