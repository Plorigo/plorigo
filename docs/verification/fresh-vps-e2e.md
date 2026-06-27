# Verification: fresh VPS → first reachable app (PLO-96)

> [!IMPORTANT]
> This is the **release gate** for the fresh-server promise: a user starts from a bare,
> supported VPS and reaches a running app **without preparing the server by hand**. It must be
> run against **real VPSes** — it does **not** pass on mocks. Record the results (images,
> commands, date, outcome) in the PR that claims the gate, using the template at the bottom.

Plorigo prepares servers two ways (see [server-management.md](../architecture/server-management.md)):

- **Manual one-line command** — the user runs `install-agent.sh` on the box themselves.
- **Dashboard-managed SSH setup** — the user enters fresh-VPS credentials and Plorigo prepares
  the box over SSH ([PLO-93](../architecture/server-management.md)).

This runbook verifies **both** reach the `ready` readiness state
([PLO-95](../architecture/agent.md), `internal/agents` `Agent.Readiness`), that a deployed app
is reachable, that failures are legible, and that re-running setup is idempotent.

## Verification levels (where this fits)

| Level | What | Where it runs |
|---|---|---|
| Hermetic | `install-agent.sh` branches + this runbook's **driver logic & secret redaction** | **CI** — `make test` (`install_agent_shim_test.go`, `e2e_fresh_vps_shim_test.go`) |
| Real Docker/Caddy | Agent register/resume; build-from-Git deploy | `make e2e-agent`, `make e2e-build` (local, not CI) |
| **Real VPS (this doc)** | **Fresh bare VPS → ready → reachable app** | **manual**, two real VPSes |

The driver (`scripts/e2e-fresh-vps.sh`, `make e2e-fresh-vps`) automates the orchestration; this
doc is the authoritative procedure and the place to record the run.

## Prerequisites

- **Two fresh VPSes**, freshly imaged, never prepared:
  - **Ubuntu 22.04 LTS** (`jammy`) — for the **manual** path.
  - **Ubuntu 24.04 LTS** (`noble`) — for the **managed SSH** path.
  - Each ≥ 1 vCPU / 1 GiB RAM / 10 GiB disk, with ports **22, 80, 443** reachable and a
    sudo-capable login (`root`, or a user with passwordless sudo).
- A **publicly-reachable control plane** the agents can dial **out** to, and that can dial
  **in** over SSH to the 24.04 box for the managed path. (`docker compose -f
  deploy/docker-compose.yml up`, behind a public URL — set `PLORIGO_PUBLIC_URL` /
  `PLORIGO_BASE_URL`. A loopback dev control plane will **not** work: the VPSes can't reach it.)
- Local tools for the driver: `curl`, `ssh`, `jq`.
- An **API auth header** for the dashboard API — log in to the dashboard and copy the session
  cookie (`Cookie: plorigo_session=…`) or use an API token (`Authorization: Bearer …`), and the
  target **workspace id**.

## The acceptance checks

### AC1 — manual command on Ubuntu 22.04 → ready

1. In the dashboard, **Connect server → Run a command**, name it, copy the one-liner. (Or call
   `AgentService/CreateRegistrationToken` and use the returned `installCommand`.)
2. Run it on the 22.04 box:
   ```sh
   curl -fsSL https://raw.githubusercontent.com/Plorigo/plorigo/main/scripts/install-agent.sh \
     | sudo sh -s -- --control-plane https://<your-cp> --token <one-time-token>
   ```
3. **Expect:** the installer prepares Docker + Caddy, installs the agent's systemd unit, starts
   it; the card flips **online**, then **ready**.

### AC2 — dashboard-managed SSH setup on Ubuntu 24.04 → ready

1. **Connect server → Plorigo sets it up**; enter the 24.04 box's host, port, username, and the
   one-time bootstrap password or private key. (The credential is used once and never stored.)
2. **Expect:** the setup timeline runs *connecting → detecting OS → checking access → pre-flight
   → installing Docker, Caddy & agent → provisioning the `plorigo` user → waiting for heartbeat
   → ready*; the card flips **online**, then **ready**.

### AC3 — what "ready" proves

`ready` is only reported when the agent's heartbeat says Docker is reachable, **Caddy is
installed and serving** (its admin API answers — so ports 80/443 are bound), the systemd
service is active (the heartbeat itself proves the outbound channel), and host resources are
healthy. A missing/old/unsafe prerequisite shows as **degraded** or **blocked** with a
plain-English reason, never a bare failure.

### AC4 — deploy a simple app and reach it

1. Add a project + service from a simple public repo (e.g. a tiny Dockerfile or a Node/Vite app
   — the build-from-Git flow, [deployment-engine.md](../architecture/deployment-engine.md)),
   targeting the prepared server; deploy.
2. **Expect:** the deploy timeline reaches **Running**; the service's generated address (its
   `*.<base-domain>` route through Caddy) serves the app.
   ```sh
   curl -fsS -o /dev/null -w '%{http_code}\n' https://<service-route>   # expect 2xx/3xx
   ```

### AC5 — failures are legible

Induce a failure and confirm the dashboard shows a plain-English reason **with raw details one
click away** (no secrets in logs):

- **Managed setup failure:** point managed setup at an unreachable host or a non-Ubuntu box →
  the run ends `failed` with the reason (e.g. *"unsupported operating system…"*), raw step log
  available.
- **Deploy failure:** deploy a repo with no Dockerfile and no recognized framework → the deploy
  ends `failed` with the reason; build/runtime logs available.

### AC6 — re-running setup is idempotent

Re-run the manual install command on the **same** 22.04 box.

- **Expect:** the server stays **online/ready**, the **same** agent identity (one agent row, no
  duplicate), and Docker, the Caddy config, the `plorigo` management user, and running
  containers are untouched. Re-running only rotates the agent credential.

### AC7 — record the run

Fill in the template below in the PR: exact VPS images, the commands/paths used, and any
limitations encountered.

## Driver

`scripts/e2e-fresh-vps.sh` automates AC1/AC2/AC6 and the AC4 reachability curl. Print the plan
first (touches nothing, reveals no secrets), then run live:

```sh
export PLORIGO_CP_URL="https://<your-cp>"
export PLORIGO_AUTH_HEADER="Cookie: plorigo_session=…"   # or "Authorization: Bearer …"
export PLORIGO_WORKSPACE_ID="…"
export E2E_MANUAL_SSH="root@<22.04-ip>"
export E2E_MANAGED_HOST="<24.04-ip>"; export E2E_MANAGED_USER="root"
export E2E_MANAGED_PASSWORD="…"        # or E2E_MANAGED_KEY_FILE=/path/to/key
export E2E_APP_URL="https://<service-route>"   # optional, for AC4

E2E_DRYRUN=1 make e2e-fresh-vps        # print the plan
make e2e-fresh-vps                     # live run
```

The driver redacts the auth header, bootstrap password, and tokens from all output. The app
**deploy** itself (AC4 setup, AC5) is driven from the dashboard per the steps above; the driver
verifies connect → ready → idempotent and curls the route you pass in `E2E_APP_URL`.

## Limitations

- Requires two **real VPSes** and a **publicly-reachable** control plane (the managed path needs
  inbound SSH from the control plane to the VPS); it cannot run in CI or against a loopback dev
  server. Its **logic** is the only part covered in CI (the hermetic driver test).
- AC4/AC5 app deploy + reachability are driven from the dashboard; the driver only curls the
  resulting route. Reachability over HTTPS assumes DNS/TLS for the base domain is configured.

## Verification record (fill in for the release PR)

```
Date:                __________
Control plane URL:   __________ (commit: ________)
22.04 image:         __________ (provider/region: ________)
24.04 image:         __________ (provider/region: ________)
AC1 manual → ready:        PASS / FAIL  (notes: ______)
AC2 managed SSH → ready:   PASS / FAIL  (notes: ______)
AC3 readiness facts:       PASS / FAIL  (Docker __, Caddy __, systemd __, heartbeat __)
AC4 app reachable:         PASS / FAIL  (route: ______)
AC5 failures legible:      PASS / FAIL  (notes: ______)
AC6 idempotent re-run:     PASS / FAIL  (same agent id: ______)
Limitations / follow-ups:  __________
```
