# Production Readiness Doctor

The **readiness** module (`internal/readiness/`) answers one question for a service or a whole
environment: *is this safe to launch to production, and if not, what should I fix next?* It is the
first slice of the Production Readiness Doctor from the product plan — deliberately small, deterministic,
and honest.

## Design contract

- **Deterministic, never heuristic.** Every check is a pure function of current platform state.
  Given the same state, the doctor returns the same verdict. There is no scoring model and no
  randomness.
- **Read-only and computed on demand.** Readiness is *derived*, exactly like a server's
  `Agent.Readiness` — it is never stored. There is no `readiness_checks` table; a cached verdict
  would only go stale. (The table is listed as *planned* in `data-and-api.md`; we intentionally do
  not build it yet.)
- **Plain English first, with the fix.** Each check carries a one-line `detail` and, when it isn't
  passing, a `remediation` telling the user the next concrete step — per
  [principles.md](./principles.md).
- **A clear severity split.** `critical` (should block production by default), `warning` (proceed
  with acknowledgement), `info`. The overall verdict is derived: any failing **critical** check →
  `not_ready`; else any **warning** → `almost_ready`; else `ready`.

## v1 scope — what it checks

All checks read **state**, not source code:

| Category | Signal | Source module |
|---|---|---|
| `deployment` | latest deployment running / failed / never deployed | deployments |
| `config` | variables whose values still look like placeholders (`changeme`, `your-…`, empty) | config |
| `domain` | public service served over HTTPS; custom-domain DNS/SSL verified | domains, services |
| `server` | a connected server is ready to deploy onto | agents |
| `backup` | a managed database has at least one backup (database services only) | backups *(optional)* |

The `backup` port is **optional**: until the backups module is wired, the backup check degrades to
an informational *"not available yet"* rather than failing — so this module ships before backups do.

## v1 non-goals (deliberate)

- **No scanning of source code or logs for secrets.** Hardcoded-secret detection is heuristic and
  prone to false positives, which would break determinism and erode trust. It is explicitly out of
  v1. (The product plan lists it under a later, richer doctor.)
- **No auth/payments/email reachability probes.** Those are "should-have" in the plan, not MVP.
- The doctor only inspects **variable** values; secret values are never returned by any RPC, so a
  secret check can confirm a key exists but never read or judge its value — the right confidentiality
  boundary (see [security.md](./security.md)).

## Shape

A **decision-only** module per [modules.md](./modules.md): it owns no tables, so it has **no
`postgres.go`**. It reads sibling state through consumer-defined ports (`ServiceReader`,
`ConfigReader`, `DomainReader`, `DeploymentReader`, `ServerReader`, and the optional `BackupReader`)
that `internal/app` satisfies with thin adapters over each module's `Service()` — so readiness
imports no sibling module. It authorizes `ActionReadinessRead` (granted to every role that can read
services), and every underlying read authorizes itself too.

The dashboard renders the checklist on the service page: the overall verdict first, then each check
with its severity, plain-English detail, and remediation.
