# Engineering principles

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

These are the invariants behind every other architecture doc. When a design decision is
unclear, choose the option that best upholds these.

1. **Own-server should not mean unsafe-server.** The platform controls servers, secrets,
   deployments, logs, terminals, and databases. Security is foundational, not a later feature —
   see [security.md](./security.md).

2. **Every scary action has a recovery path.** Deploys have rollbacks, databases have backups,
   migrations have checkpoints, and production changes leave an audit trail. If you add a risky
   operation, add its undo.

3. **Progressive disclosure.** Lead with a clear, plain summary; keep the raw details (logs,
   config, Docker internals, terminal access) always reachable. Beginners get guidance; power
   users are never blocked. This is both an engineering and a UI rule — see [dashboard.md](./dashboard.md).

4. **Preview first, production second.** Every app should have a safe place to test before it
   reaches production.

5. **AI agents can help, but are not trusted blindly.** Agents may read logs, create previews,
   and suggest fixes; dangerous production actions require explicit human approval. The
   enforcement model is in [security.md](./security.md).

6. **Backups must be visible, testable, and restorable.** "Backup enabled" is not enough —
   design for restore confidence, not just for taking backups.

7. **Plain English first, raw details always available.** Explain failures simply, but never
   hide the underlying technical data from people who want it.

These principles come straight from the product's design intent; they're stated here as
engineering guidance so code reviews can point at them.
