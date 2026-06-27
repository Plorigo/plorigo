# Server management & SSH

> [!NOTE]
> **Status: target architecture (design contract).** Plorigo is in early development; much of
> what's described here is not yet built. This document defines the *intended* design — write
> code to match it, and update this doc in the same change when the design needs to evolve.
> For scope and sequencing see [ROADMAP.md](../../ROADMAP.md); this is **not** a feature
> commitment or a description of shipped functionality.

> [!IMPORTANT]
> This doc introduces an **inbound** path to a user's server — the one thing the
> [agent trust model](./agent.md#trust-model) is built to avoid. Read it, [agent.md](./agent.md),
> and [security.md](./security.md) before touching anything that opens an SSH connection, stores
> an SSH credential, or runs a setup job.

Plorigo deploys through an **outbound agent**: a small program on the server dials the control
plane and pulls signed, scoped jobs, so the control plane never needs to reach into the server
(see [agent.md](./agent.md)). That keeps the blast radius small, but it assumes the agent is
*already installed*. Getting it there — and repairing it when it breaks — is what this document
covers.

There are two ways a server gets prepared:

- **Self-serve one-line install** (`scripts/install-agent.sh`): the user runs a command on their
  own box. Plorigo stores **no** SSH credential; this path is unchanged and preferred for
  technical users.
- **Dashboard-managed setup** (the subject of this doc): the user enters fresh-VPS connection
  details and Plorigo prepares the server for them — installing prerequisites, creating a
  least-privilege user, and starting the agent — over SSH.

The dashboard-managed path needs a **persistent SSH management channel**. This document defines
that channel as a **controlled management/repair tool, never the deployment channel**, and
specifies the credential lifecycle, least-privilege defaults, host-key handling, audit,
redaction, and recovery that keep it from becoming an unbounded backdoor. Every scary action
keeps a recovery path — see [principles.md](./principles.md).

## Two channels, clearly separated

The agent channel and the SSH channel are different tools with different trust properties. They
must never be conflated:

| | Outbound agent channel | SSH bootstrap / management channel |
|---|---|---|
| **Direction** | Agent dials **out** to the control plane | Control plane dials **in** to the server |
| **When** | Every deploy; always-on | Setup and repair only; on demand |
| **Carries** | Signed, scoped deployment jobs | Setup steps / management commands |
| **Credential** | Agent ed25519 key + credential hash (control plane holds only the **public** key + hash) | Generated `plorigo` user + a dedicated SSH key (control plane holds the **private** key, sealed) |
| **Lifetime** | Continuous | Idle between uses; rotatable and revocable |
| **Primary?** | **Yes — this is the deploy path** | **No — never the deploy path** |

The asymmetry in the credential row is the whole point: the agent channel stores nothing that
can act on the server, while the SSH channel necessarily does. Everything below exists to bound
what that stored credential can do.

## When SSH is used (and when it is not)

The SSH channel is used for exactly two things:

1. **Bootstrap** a fresh server: connect with user-supplied credentials, verify the OS, install
   prerequisites / Docker / Caddy, create the `plorigo` management user, install the agent and
   its systemd unit, and start it. This reuses the same installer the one-line path runs
   (`scripts/install-agent.sh`), which already prepares a bare Ubuntu LTS box (Docker, Caddy,
   directories, the systemd unit, and verification); the dashboard path drives it over SSH and
   adds the non-root `plorigo` management user. (The managed-setup flow itself is a later step —
   see [ROADMAP.md](../../ROADMAP.md).)
2. **Manage / repair** an existing server: re-run prerequisites, restart or recover a stuck
   agent, or rotate the management key.

It is **not** used for anything else. Once the agent is online, deployments, log streaming,
Caddy, and health all flow over the **outbound agent channel** — the control plane does not SSH
in to deploy. If the agent is healthy, the SSH channel sits idle.

## Credential lifecycle

This is the core of the model. There are **two** credentials, with deliberately different
lifetimes.

### Raw bootstrap credential (transient)

The user supplies a one-time credential for the fresh box — typically `root` (or a sudo-capable
user) with a password or private key. This credential is **powerful and untrusted**, so:

- It is held **in memory for the active setup attempt only**.
- It is **never written to disk, never persisted to the database, never logged**.
- It is **discarded when setup completes — on success *and* on failure**. There is no "remember
  this password" box. To retry after a failure, the user re-enters it.

### Generated management credential (persistent)

During bootstrap, while it still holds the raw credential, Plorigo provisions the durable
identity it will use from then on:

- It creates a dedicated **non-root `plorigo` user** on the server.
- It generates a **fresh ed25519 keypair used only for SSH management** — **distinct from the
  agent's job-signing keypair**. The two never share key material; compromising one must not
  imply the other.
- The **public** key is installed into `/home/plorigo/.ssh/authorized_keys`.
- The **private** key is **sealed at rest** with the same AES-256-GCM box and `APP_MASTER_KEY`
  used for secrets (see [security.md](./security.md) and `internal/platform/crypto`). It is
  opened only in-process, only when the SSH channel actually runs.

After this, the raw bootstrap credential is discarded. The sealed `plorigo` key is the **only
persistent SSH credential** Plorigo holds for the server.

### Rotation

Rotation generates a new keypair, installs the new `authorized_keys` entry, removes the old one,
and replaces the stored ciphertext — all in one audited operation. A failed rotation must leave
a working key in place (never strand the server).

### Revocation

Revocation removes the `plorigo` `authorized_keys` entry and marks the stored credential
revoked; SSH access ends immediately. The dashboard exposes **Rotate key** and **Revoke SSH
access** as first-class, user-driven actions — the user is always in control of the channel into
their own server.

## Least-privilege defaults for the `plorigo` user

The generated user is deliberately *not* root:

- **Dedicated, non-root** account (`plorigo`), used only by Plorigo's management channel.
- **Key-only SSH** — no password authentication for `plorigo`.
- **Scoped sudo via a managed `/etc/sudoers.d/plorigo` drop-in with an explicit command
  allowlist** — only what bootstrap and repair actually need (package/prerequisite installs,
  Docker, Caddy, and the agent's systemd unit). **Not** `ALL=(ALL)`.

> [!WARNING]
> Two things here are effectively root and must not be granted casually:
> - **Docker group membership ≈ root.** A member of the `docker` group can mount the host
>   filesystem into a container and escalate. Prefer **scoped sudo for specific `docker`
>   subcommands** over adding `plorigo` to the `docker` group; if group membership is
>   unavoidable, treat it as a root grant.
> - **A broad sudoers allowlist ≈ root.** Wildcards or shell-spawning commands in the drop-in
>   defeat the point. The exact allowlist is a **security-review item** for the setup
>   implementation — see [Security review](#security-review--residual-risk).

## Host-key handling

The control plane verifies it is talking to the right machine, every time, using
**trust-on-first-use (TOFU) with pinning**:

- On the **first** connection (bootstrap), capture the server's host-key fingerprint and **pin**
  it on the server record.
- On **every** subsequent connection, enforce strict host-key checking against the pinned
  fingerprint (no blind `StrictHostKeyChecking=no`).
- **Surface the fingerprint in the dashboard** so a careful user can verify it out of band
  against what their provider shows.
- On a **mismatch** — which can mean a rebuilt host *or* a man-in-the-middle — **refuse to
  connect** and require explicit user re-confirmation before re-pinning. The mismatch default is
  a [security-review item](#security-review--residual-risk): it must be *refuse*, not *re-pin
  silently*.

## Audit events

Every use of the SSH channel is auditable. The implementation records these via the `audit`
module, each with the **authenticated actor** and the **server / workspace scope**:

- SSH connection opened (with the host-key fingerprint used).
- Setup run **started**, each **step**, and **success / failure / retry**.
- Management credential **installed**.
- **Prerequisite change** (package/Docker/Caddy install or upgrade).
- Key **rotation** and **revocation**.
- **Failed authentication** against the server.

These join the same append-only audit trail as deploys, secret changes, and terminal sessions
(see [security.md](./security.md#audit-approvals-recovery)).

## Redaction

The SSH channel handles the most sensitive material in the product, so it follows the secret
discipline without exception. **Raw bootstrap credentials, the `plorigo` private key,
registration tokens, and any secret values inside command output are never** logged, returned by
any RPC, or written to the audit trail. Stored credentials are **write-only through the API** —
no RPC returns private key material; only non-secret metadata (fingerprint, timestamps,
rotation/revocation state) is readable. This mirrors how `secrets` and source credentials are
handled in [security.md](./security.md#secrets).

## Failure recovery

Bootstrap touches a real machine that may be in any state, so it is built to be re-run:

- **Idempotent and resumable.** Re-running setup for reconnect, repair, or rotation must not
  duplicate the `plorigo` user, keys, Docker installation, Caddy state, or systemd units.
- **No stranded state on failure.** The raw credential is discarded even when setup fails; a
  partially-prepared server is safe to re-run against.
- **Plain-English errors** for the known failure modes — apt lock held, missing sudo/root,
  unsupported OS, Docker install failure, ports 80/443 occupied, agent heartbeat timeout — each
  with a recovery hint rather than a raw stack trace (see [principles.md](./principles.md)).
- **The channel is itself a recovery path.** When an agent is wedged and the outbound channel is
  dead, the SSH management channel is how Plorigo gets back in to fix it — which is exactly why
  it is worth having, and worth bounding this carefully.

## Why this is not the design the agent model bans

[agent.md](./agent.md#trust-model) bans "long-lived **root** SSH keys stored in the control
plane." The persistent credential defined here is deliberately **not** that:

| The banned thing | What this model stores instead |
|---|---|
| Root | A **non-root** `plorigo` user |
| Unrestricted shell | **Scoped sudo** via an allowlisted drop-in |
| Plaintext / long-lived | **Sealed at rest**, **rotatable**, **revocable** |
| Unverified peer | **Host-key pinned** (TOFU + strict checking) |
| Silent | **Audited** on every use, **redacted** everywhere |
| On the deploy hot path | **Idle except for setup/repair**; deploys stay on the agent |

The residual risk is real and named: a credential exists that **can** reach the server. The
mitigations above (least privilege, encryption at rest, rotation/revocation, host-key pinning,
audit, and keeping it off the deploy path) are what make that risk acceptable for the
convenience it buys. Where implementation could still get this wrong, it is flagged for review
below.

## Illustrative implementation sketch

> [!NOTE]
> This section is **illustrative, not binding.** It sketches one shape so reviewers can see the
> model is buildable; the real schema, module layout, and contracts are owned by the follow-up
> implementation work (encrypted credential storage first, then the bootstrap jobs — see
> [ROADMAP.md](../../ROADMAP.md)) and must follow the boundary rules in [modules.md](./modules.md)
> and the schema conventions in [data-and-api.md](./data-and-api.md).

**Data model.** A management-credential record scoped to a workspace/server, holding: key
**fingerprint**, **sealed private key** bytes, `last_used_at`, **rotation state**, `revoked_at`,
and `created_by`. The **host-key fingerprint** is pinned on the `servers` row. No raw bootstrap
credential is ever a column.

**Module placement.** A new `internal/serversetup` module (or an extension of `internal/servers`)
that owns the setup run and the SSH credential, reusing the existing `crypto.Box` sealer,
calling `policy.Authorize` before any privileged action, and recording through `audit` in the
same transaction — the same seams `secrets` and `sources` already use.

**Contracts.** A `ServerSetupService` (ConnectRPC) that starts a setup run from
`host` / `port` / `username` / one-time bootstrap auth and streams **ordered, redacted** step
status to the dashboard, plus management RPCs to **rotate**, **revoke**, and **inspect**
(metadata only) the credential.

## Scope & security review

- **First target:** Ubuntu 22.04 / 24.04 LTS; unsupported OSes are rejected with a clear reason.
- **Out of scope:** provider-API provisioning (Plorigo starts from a server the user already
  has). See [ROADMAP.md](../../ROADMAP.md) for sequencing.

### Security review — residual risk

Bare-server preparation has shipped in the one-line installer (`scripts/install-agent.sh`), and
encrypted storage of the management credential — sealed private key, fingerprint, last-used,
rotation and revocation state, with workspace-scoped authorization and an audit record for every
provision, use, rotation, revocation, and failed auth — has shipped in the `internal/serversetup`
module (write-only: no RPC returns the private key). The implementation work that still follows
this model — **running setup/repair over SSH** (connecting, installing/removing the
`authorized_keys` entry, host-key TOFU pinning on the `servers` row; see
[ROADMAP.md](../../ROADMAP.md)) — carries the risk that remains. Each of the following must get
**explicit security review** before it ships:

- **The installer's package sources** — Docker and Caddy are installed from their official apt
  repositories with pinned, `signed-by=` keyrings under `/etc/apt/keyrings`; the repo and key URLs
  are documented in the installer header and must stay canonical and current.
- **The sudoers allowlist** — must be minimal and free of wildcards / shell-spawning commands,
  since a loose allowlist is root-equivalent.
- **The host-key-mismatch default** — must *refuse and require confirmation*, never silently
  re-pin.
- **Key-at-rest handling** — sealing, rotation, revocation, redaction, and cross-workspace
  access denial, with tests.
