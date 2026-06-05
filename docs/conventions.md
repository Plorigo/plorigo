# Engineering conventions

> [!NOTE]
> This page consolidates the day-to-day engineering norms and links to the canonical sources.
> For the full contribution process (CLA, PR template, review), see
> [CONTRIBUTING.md](../CONTRIBUTING.md) — this doc does not replace it.

## Formatting (EditorConfig)

The repo ships an [`.editorconfig`](../.editorconfig); your editor should honor it. In short:

- **2-space indent** by default (YAML, JSON, TS/TSX, JS/JSX, CSS, HTML).
- **Go** uses **tabs**; **Makefiles** use tabs.
- **LF** line endings, **UTF-8**, and a **final newline** in every file.
- Trailing whitespace is trimmed everywhere except Markdown.

## Go

- Code must be **`gofmt`/`goimports` clean** and pass **`golangci-lint`**.
- Prefer **small, well-tested packages**.
- **Handle errors explicitly.** Never ignore an error from a privileged operation (Docker,
  Caddy, filesystem, secrets) — these run on users' production systems. See
  [architecture/security.md](./architecture/security.md).

## TypeScript / React

- Follow the project **ESLint/Prettier** config.
- Keep components **typed and accessible**.
- Consume the backend through the **generated ConnectRPC client** — see
  [architecture/dashboard.md](./architecture/dashboard.md).

## Generated code — never hand-edit

Two generators own their output; edit the **source** and regenerate:

- **Protobuf / ConnectRPC** via **`buf`** (`buf generate`) — `.proto` is the source.
- **SQL** via **`sqlc`** — the `.sql` query/schema is the source; run a schema migration with
  **`goose`** and regenerate. See [architecture/data-and-api.md](./architecture/data-and-api.md).

Generated files (`*.pb.go`, `*_connect.go`, `proto/gen/**`, etc.) are marked
`linguist-generated` in [`.gitattributes`](../.gitattributes) and are collapsed in diffs.

## Commits & PRs

- **[Conventional Commits](https://www.conventionalcommits.org/)** for commit messages and PR
  titles. PRs are **squash-merged**, so the PR title becomes the commit:
  - `feat: add per-branch preview URLs`
  - `fix(agent): reconnect after control-plane restart`
  - `docs: clarify backup retention`
- Branch off `main` (protected; all changes land via PR). Keep PRs focused.

## Testing

- Add or update tests for the behavior you change; bug fixes come with a **regression test**.
- Run the relevant suites before pushing (`go test ./...`, `pnpm test`).
- For changes to the **deployment, agent, secrets, or backup** paths, **test against a real
  Docker environment** and describe how in your PR — these paths affect users' production systems.

## The safety-review lens

Reviewers pay special attention to anything touching **privileged operations** — Docker, host
commands, secrets, backups, the agent, and the AI/MCP gateway. **Any change that broadens what
an AI agent or an unprivileged user can do gets extra scrutiny.** Before writing such a change,
read [architecture/security.md](./architecture/security.md) and the
[engineering principles](./architecture/principles.md).

## Documentation lives with the code

The [`docs/architecture/`](./architecture/) docs are a **design contract**. When you change a
design, **update its doc in the same PR**. And remember the source boundary in
[AGENTS.md](../AGENTS.md): this is a public repo — document engineering, not feature roadmaps,
pricing, or business strategy (link [ROADMAP.md](../ROADMAP.md) for roadmap-ish topics).
