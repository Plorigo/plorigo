# How to add a module

> [!NOTE]
> **Status: target architecture (design contract).** This is the canonical guide for
> adding a control-plane module. The `projects` module is the working reference
> implementation; copy it. When the pattern needs to change, change `projects` and this
> doc in the same PR.

The control plane is a [modular monolith](./control-plane.md). Each module owns one
slice of the domain, lives in a single Go package under `internal/<module>/`, and is
wired together in `internal/app`. This guide is the contract that keeps the monolith
**modular as it grows** — follow it exactly.

## The file layout (copy `internal/projects/`)

| File | Responsibility |
|---|---|
| `projects.go` | Domain types + the exported `Service` interface. |
| `store.go` | The module's **ports**: `Store` (its repository) and any **consumer-defined** interfaces it needs from other modules. |
| `postgres.go` | `Store` adapter over the shared sqlc package. **The only file allowed to import `internal/platform/database/db`.** |
| `service.go` | Business logic. Orchestrates ports only — no SQL, no transport. |
| `handler.go` | ConnectRPC handler. Maps proto ⇄ domain and domain errors → connect codes. No business logic. |
| `module.go` | `Deps` + `New(Deps) *Module`; exposes `Service()` and `Route()`. The only wiring surface. |
| `service_test.go` / `handler_test.go` | Table tests with fakes — no database. |

A provider-only module with no RPC surface (like `audit` or `membership`) is the
minimal shape: `<module>.go` + `store.go` + `postgres.go`, returning a concrete
`*Service` from `New`. A **decision-only** module (`policy`) goes one step further — it
owns no tables, so it has no `postgres.go`: just its domain, the `Authorize` logic, and
its consumer-defined ports. A module that serves more than one proto service (like
`projects`, which owns `ProjectService` **and** `WorkspaceService`) exposes one
`Route()`-style method per service, all mounted in `internal/app/router.go`.

## Rule 1 — modules never import each other

This is the rule that makes "modular monolith" real (and makes later extraction into a
service possible). A module **must not** import another `internal/<module>` package.

When module A needs something from module B, A declares a **small interface in its own
package** describing only what it needs (a *consumer-defined port*). `internal/app`
injects B's concrete `Service`, and Go structural typing satisfies the port — A never
imports B.

`internal/services` is a clean second example: its `CreateService` can kick off a first
deployment, so it declares an `Enqueuer` port (just the one method it needs) that
`*deployments.Service` satisfies structurally — `services` never imports `deployments`. It
reuses the same GitHub-client port and crypto `SecretBox` as the `sources` module to
validate a git source.

```go
// internal/projects/store.go — projects declares what it needs from audit.
type Recorder interface {
    Record(ctx context.Context, tx database.Tx, action, targetType, targetID, workspaceID, actor string) error
}
```

```go
// internal/app/modules.go — the single place that wires modules together.
auditSvc := audit.New(audit.Deps{Log: a.log})
a.projects = projects.New(projects.Deps{
    DB:    a.db,
    Audit: auditSvc, // *audit.Service satisfies projects.Recorder structurally
    Log:   a.log,
})
```

**Enforced by depguard** (`.golangci.yml`): any cross-module import fails `make lint`.
The single `module-boundaries` rule lists every module's directory and package; when you
add a module, add it to both lists. (depguard matches whole packages, not symbols, which
is exactly why we forbid the import entirely rather than "allow only the Service
interface".)

## Rule 2 — only `postgres.go` touches the database

`sqlc` generates one shared package, `internal/platform/database/db`. Only a module's
`postgres.go` may import it; everything else depends on the module's `Store` port. Also
depguard-enforced.

## Rule 3 — an action and its audit record commit together

Sensitive actions are audited (see [security.md](./security.md)). The audit row must be
written in the **same transaction** as the action, so there is never an un-audited
change. Use `database.WithinTx` and pass the `tx` to both the store and the recorder:

```go
err := s.tx.WithinTx(ctx, func(tx database.Tx) error {
    created, err = s.store.InsertProject(ctx, tx, candidate)
    if err != nil {
        return err
    }
    return s.audit.Record(ctx, tx, "project.create", "project", created.ID, created.WorkspaceID, actor)
})
```

## Rule 4 — the authorization seam (follow it for every privileged module)

`auth` and `policy` now exist (see [auth.md](./auth.md)), so the seam is real and
`projects` is privileged. A privileged module (services, deployments, secrets, agents,
docker, caddy, backups, terminal, ai, mcp — see [control-plane.md](./control-plane.md))
must authorize **before** mutating:

- the service reads the caller with `principal.FromContext(ctx)` (the interceptor in
  `internal/app` put it there);
- it calls `authz.Authorize(ctx, caller, action, resource)` **before** the `WithinTx`
  block, and audits the actor (`caller.UserID`) **inside** it.

The seam is built from **neutral platform packages** so no module imports another:
`internal/platform/principal` carries identity through the context, and
`internal/platform/authz` holds the `Action`/`Resource` vocabulary, the `Role`
constants, and the `Authorizer` interface. `policy` *implements* `authz.Authorizer`;
consumers *depend on* it. `policy` reads roles through a consumer-defined
`MembershipReader` satisfied by the provider-only `membership` module — that indirection
is what breaks the `projects ↔ policy` cycle (`projects → policy → membership`, never
back). Wire these in `internal/app` in dependency order.

Authorization is **workspace-scoped**, but a resource can sit below the workspace in the
tree — an environment belongs to a project, not directly to a workspace. A module for
such a resource resolves its owning `workspace_id` in its own `store` (a lookup/JOIN on
the ancestor table — see `internal/environments`) and authorizes against that. It does
**not** import the ancestor's module: reading the shared database from `postgres.go` is
exactly what Rule 2 permits.

Any change that broadens what an unprivileged user or AI agent can do gets extra review
([security.md](./security.md)).

## Conventions

- **IDs are strings.** `sqlc` maps `uuid → string` and proto uses strings, so domain IDs
  stay strings end-to-end. Use `internal/platform/id` to generate/validate them.
- **Errors.** Return `internal/platform/problem` errors from the domain; `handler.go`
  maps them to connect codes via `problem.ToConnect`. Never leak internal details.
- **`internal/agents` vs the agent program.** `internal/agents` is the *control-plane*
  module (registration, keys, job gateway). The agent **program** is `cmd/agent` +
  `internal/agentcore`. They are different things — don't conflate them. The CLI is
  likewise `cmd/cli` + `internal/clicore`.

## Checklist for a new module

1. `cp -r internal/projects internal/<module>` and rename.
2. Add its proto under `proto/controlplane/v1/` and `make generate`.
3. Add its migration + `db/query/<module>.sql` and `make generate`.
4. Define ports in `store.go`; declare consumer-defined ports for collaborators.
5. Add a `module-<module>` depguard block in `.golangci.yml`.
6. Construct it and wire collaborators in `internal/app/modules.go`; mount `Route()` in
   `internal/app/router.go`.
7. `make generate && make test && make lint` all green.
