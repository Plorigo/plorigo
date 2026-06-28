#!/bin/sh
# e2e-fresh-vps.sh — drive the "fresh VPS to first reachable app" verification (PLO-96).
#
# This is the release gate for the fresh-server promise. It is NOT run in CI and does NOT
# pass on mocks: it exercises REAL Ubuntu VPSes against a REAL, publicly-reachable control
# plane. Its logic is covered hermetically by internal/app/e2e_fresh_vps_shim_test.go (which
# runs this script with --dry-run and fakes); the live run here is the manual sign-off.
#
# It verifies, end to end, that:
#   1. A fresh Ubuntu 22.04 VPS connected with the manual one-line command reaches "ready".
#   2. A fresh Ubuntu 24.04 VPS connected with the dashboard-managed SSH path reaches "ready".
#   3. Re-running setup is idempotent (one agent identity, no broken Docker/Caddy/units).
#   4. (Optionally) a deployed app is reachable at its route.
# The exact images/commands/limitations are documented in docs/verification/fresh-vps-e2e.md;
# read it before running. The actual app DEPLOY is driven from the dashboard (see the runbook);
# this driver verifies connect → ready → idempotent, and curls the app route if you pass one.
#
# Usage:
#   scripts/e2e-fresh-vps.sh [--dry-run]
#
# Required environment:
#   PLORIGO_CP_URL        Public control-plane URL the agents reach, e.g. https://cp.example.com
#   PLORIGO_AUTH_HEADER   An HTTP auth header for the dashboard API, e.g.
#                         "Authorization: Bearer plo_…" or "Cookie: plorigo_session=…".
#                         See the runbook for how to obtain it.
#   PLORIGO_WORKSPACE_ID  Workspace UUID to connect the servers into.
#   E2E_MANUAL_SSH        ssh target for the 22.04 box, e.g. root@203.0.113.10
#   E2E_MANAGED_HOST      host/IP of the 24.04 box (the control plane SSHes IN to it)
#   E2E_MANAGED_USER      bootstrap SSH user on the 24.04 box (e.g. root)
# One of (the 24.04 box's one-time bootstrap credential — used once, never stored):
#   E2E_MANAGED_PASSWORD  bootstrap password, or
#   E2E_MANAGED_KEY_FILE  path to a bootstrap private key (PEM)
#
# Optional environment:
#   E2E_MANUAL_SSH_OPTS   extra ssh options for the 22.04 box (e.g. "-i key -p 2222")
#   E2E_MANAGED_PORT      SSH port for the 24.04 box (default 22)
#   E2E_APP_URL           a deployed app's URL to curl for reachability (after you deploy it)
#   E2E_READY_TIMEOUT     seconds to wait for "ready" (default 300)

set -eu

DRY_RUN=0
[ "${1:-}" = "--dry-run" ] && DRY_RUN=1

PASS_COUNT=0
FAIL_COUNT=0

# --- output helpers -----------------------------------------------------------------------

log()  { printf '\033[0;34m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[0;33mwarning:\033[0m %s\n' "$1" >&2; }
ok()   { printf '\033[0;32m  PASS\033[0m %s\n' "$1"; PASS_COUNT=$((PASS_COUNT + 1)); }
bad()  { printf '\033[0;31m  FAIL\033[0m %s\n' "$1" >&2; FAIL_COUNT=$((FAIL_COUNT + 1)); }
die()  { printf '\033[0;31merror:\033[0m %s\n' "$1" >&2; exit 1; }

# redact hides a secret value in any string we print, so a bootstrap password / private key /
# token / auth header never lands in the verification log (mirrors scripts/install-agent.sh).
# It uses literal parameter-expansion replacement (no sed), so a secret containing regex or
# shell metacharacters can't defeat it — a redaction bug here would be a credential leak.
redact() {
  _s=$1
  for _v in "${PLORIGO_AUTH_HEADER:-}" "${E2E_MANAGED_PASSWORD:-}" "${REG_TOKEN:-}"; do
    [ -z "$_v" ] && continue
    while case "$_s" in *"$_v"*) true ;; *) false ;; esac; do
      _s="${_s%%"$_v"*}***REDACTED***${_s#*"$_v"}"
    done
  done
  printf '%s' "$_s"
}

# plan prints a command instead of running it (dry-run), redacting secrets. It writes to
# stderr so it stays visible even when api() is called inside a command substitution (whose
# stdout is captured into a variable).
plan() { printf '    plan: %s\n' "$(redact "$1")" >&2; }

# --- input validation ---------------------------------------------------------------------

require_env() {
  _missing=""
  for _k in PLORIGO_CP_URL PLORIGO_AUTH_HEADER PLORIGO_WORKSPACE_ID E2E_MANUAL_SSH E2E_MANAGED_HOST E2E_MANAGED_USER; do
    eval "_val=\${$_k:-}"
    [ -z "$_val" ] && _missing="$_missing $_k"
  done
  if [ -z "${E2E_MANAGED_PASSWORD:-}" ] && [ -z "${E2E_MANAGED_KEY_FILE:-}" ]; then
    _missing="$_missing E2E_MANAGED_PASSWORD|E2E_MANAGED_KEY_FILE"
  fi
  if [ -n "$_missing" ]; then
    die "missing required environment:$_missing (see the header / docs/verification/fresh-vps-e2e.md)"
  fi
  # A dry run only prints the plan, so it needs no real tools; a live run does.
  if [ "$DRY_RUN" = "0" ]; then
    for _bin in curl ssh jq; do
      command -v "$_bin" >/dev/null 2>&1 || die "required tool not found: $_bin"
    done
  fi
}

# --- Connect RPC helper --------------------------------------------------------------------

READY_TIMEOUT="${E2E_READY_TIMEOUT:-300}"
MANAGED_PORT="${E2E_MANAGED_PORT:-22}"

# api METHOD JSON — POST a unary ConnectRPC call (JSON over HTTP) and echo the response body.
# In dry-run it only prints the plan. Secrets in the body are redacted from the plan.
api() {
  _method=$1
  _body=$2
  _url="${PLORIGO_CP_URL%/}/controlplane.v1.$_method"
  if [ "$DRY_RUN" = "1" ]; then
    plan "curl -sS -X POST '$_url' -H '$PLORIGO_AUTH_HEADER' -H 'Content-Type: application/json' -d '$(redact "$_body")'"
    printf '{}'
    return 0
  fi
  curl -sS -X POST "$_url" \
    -H "$PLORIGO_AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "$_body"
}

# json_field extracts a field from a JSON object on stdin via jq (empty in dry-run).
json_field() { [ "$DRY_RUN" = "1" ] && { printf ''; return 0; }; jq -r "$1 // empty"; }

# --- shared helpers ------------------------------------------------------------------------

# create_server NAME -> echoes the new server id (empty in dry-run).
create_server() {
  api "ServerService/CreateServer" \
    "$(printf '{"workspaceId":"%s","name":"%s"}' "$PLORIGO_WORKSPACE_ID" "$1")" | json_field '.server.id'
}

# wait_ready SERVER_ID LABEL — poll ListAgentsByWorkspace until the server's agent reports
# status "online" and readiness "ready", or the timeout elapses. This is the core assertion:
# Docker, Caddy, the systemd service, the outbound heartbeat, and the readiness preflight all
# have to be healthy for "ready" (see internal/agents Agent.Readiness, PLO-95).
wait_ready() {
  _sid=$1
  _label=$2
  if [ "$DRY_RUN" = "1" ]; then
    plan "poll AgentService/ListAgentsByWorkspace until server $_sid agent status=online readiness=ready (timeout ${READY_TIMEOUT}s)"
    return 0
  fi
  _deadline=$(( $(date +%s) + READY_TIMEOUT ))
  while [ "$(date +%s)" -lt "$_deadline" ]; do
    _agents=$(api "AgentService/ListAgentsByWorkspace" "$(printf '{"workspaceId":"%s"}' "$PLORIGO_WORKSPACE_ID")")
    _status=$(printf '%s' "$_agents" | jq -r --arg s "$_sid" '.agents[]? | select(.serverId==$s) | .status // empty')
    _readiness=$(printf '%s' "$_agents" | jq -r --arg s "$_sid" '.agents[]? | select(.serverId==$s) | .readiness // empty')
    if [ "$_status" = "online" ] && [ "$_readiness" = "ready" ]; then
      return 0
    fi
    sleep 5
  done
  warn "$_label: server $_sid did not reach online+ready within ${READY_TIMEOUT}s (last: status=${_status:-?} readiness=${_readiness:-?})"
  return 1
}

# --- AC 1: manual one-line command on a fresh Ubuntu 22.04 box -----------------------------

step_manual() {
  log "AC1: fresh Ubuntu 22.04 via the manual one-line command"
  _sid=$(create_server "e2e-manual-2204")
  # Use the install command the control plane returns verbatim — it embeds the one-time token
  # and points at the published installer, which prepares the host (Docker, Caddy) and starts
  # the agent. Full prep, no skips.
  _resp=$(api "AgentService/CreateRegistrationToken" "$(printf '{"serverId":"%s"}' "$_sid")")
  REG_TOKEN=$(printf '%s' "$_resp" | json_field '.registrationToken')
  _install=$(printf '%s' "$_resp" | json_field '.installCommand')
  if [ "$DRY_RUN" = "1" ]; then
    plan "ssh ${E2E_MANUAL_SSH} ${E2E_MANUAL_SSH_OPTS:-} '<install command from CreateRegistrationToken>'"
  else
    [ -n "$_install" ] || { bad "AC1: no install command returned"; return; }
    # shellcheck disable=SC2086
    ssh ${E2E_MANUAL_SSH_OPTS:-} "$E2E_MANUAL_SSH" "$_install" || { bad "AC1: manual install command failed over SSH"; return; }
  fi
  if wait_ready "${_sid:-manual}" "AC1"; then
    ok "AC1: 22.04 reached online + ready via the manual command"
  else
    bad "AC1: 22.04 did not reach ready"
  fi
  MANUAL_SERVER_ID=$_sid
}

# --- AC 2: dashboard-managed SSH setup on a fresh Ubuntu 24.04 box --------------------------

step_managed() {
  log "AC2: fresh Ubuntu 24.04 via the dashboard-managed SSH setup"
  _sid=$(create_server "e2e-managed-2404")
  # Build StartSetup with exactly one of password / private_key (the one-time bootstrap cred).
  if [ -n "${E2E_MANAGED_PASSWORD:-}" ]; then
    _auth=$(printf '"password":"%s"' "$E2E_MANAGED_PASSWORD")
  else
    [ -f "${E2E_MANAGED_KEY_FILE:-}" ] || { bad "AC2: E2E_MANAGED_KEY_FILE not found"; return; }
    _key=$(jq -Rs . < "$E2E_MANAGED_KEY_FILE")
    _auth=$(printf '"privateKey":%s' "$_key")
  fi
  _req=$(printf '{"serverId":"%s","host":"%s","port":%s,"username":"%s",%s}' \
    "$_sid" "$E2E_MANAGED_HOST" "$MANAGED_PORT" "$E2E_MANAGED_USER" "$_auth")
  _runid=$(api "ServerSetupService/StartSetup" "$_req" | json_field '.run.id')
  if wait_setup_succeeded "${_runid:-managed}" && wait_ready "${_sid:-managed}" "AC2"; then
    ok "AC2: 24.04 reached online + ready via dashboard-managed SSH setup"
  else
    bad "AC2: 24.04 did not complete managed setup / reach ready (inspect ListSetupEvents for the failing step)"
  fi
}

# wait_setup_succeeded RUN_ID — poll GetSetupRun until status is succeeded/failed.
wait_setup_succeeded() {
  _rid=$1
  if [ "$DRY_RUN" = "1" ]; then
    plan "poll ServerSetupService/GetSetupRun run $_rid until status=succeeded; on failure ListSetupEvents for the reason"
    return 0
  fi
  _deadline=$(( $(date +%s) + READY_TIMEOUT ))
  while [ "$(date +%s)" -lt "$_deadline" ]; do
    _st=$(api "ServerSetupService/GetSetupRun" "$(printf '{"setupRunId":"%s"}' "$_rid")" | jq -r '.run.status // empty')
    case "$_st" in
      succeeded) return 0 ;;
      failed) warn "setup run $_rid failed"; return 1 ;;
    esac
    sleep 5
  done
  warn "setup run $_rid did not finish within ${READY_TIMEOUT}s"
  return 1
}

# --- AC 6: re-running setup is idempotent ---------------------------------------------------

step_idempotent() {
  log "AC6: re-running the manual install is idempotent (no duplicate agent / broken host)"
  [ -z "${MANUAL_SERVER_ID:-}" ] && { warn "AC6: skipped (AC1 did not create a server)"; return; }
  _resp=$(api "AgentService/CreateRegistrationToken" "$(printf '{"serverId":"%s"}' "$MANUAL_SERVER_ID")")
  REG_TOKEN=$(printf '%s' "$_resp" | json_field '.registrationToken')
  _install=$(printf '%s' "$_resp" | json_field '.installCommand')
  if [ "$DRY_RUN" = "1" ]; then
    plan "ssh ${E2E_MANUAL_SSH} '<install command>'  # second run, must preserve the agent identity"
    plan "assert exactly one agent for server $MANUAL_SERVER_ID and it stays online+ready"
    ok "AC6: idempotency plan emitted (dry-run)"
    return
  fi
  [ -n "$_install" ] || { bad "AC6: no install command returned"; return; }
  # shellcheck disable=SC2086
  ssh ${E2E_MANUAL_SSH_OPTS:-} "$E2E_MANUAL_SSH" "$_install" || { bad "AC6: re-run install failed"; return; }
  if wait_ready "$MANUAL_SERVER_ID" "AC6"; then
    ok "AC6: server still online + ready after a second install (identity preserved)"
  else
    bad "AC6: re-run left the server not-ready"
  fi
}

# --- AC 4: a deployed app is reachable -----------------------------------------------------

step_reach() {
  log "AC4: a deployed app is reachable at its route"
  if [ -z "${E2E_APP_URL:-}" ]; then
    warn "AC4: skipped — set E2E_APP_URL to a deployed app's route to verify reachability (deploy it from the dashboard per the runbook)"
    return
  fi
  if [ "$DRY_RUN" = "1" ]; then
    plan "curl -fsS -o /dev/null -w '%{http_code}' '$E2E_APP_URL'  # expect 2xx/3xx"
    ok "AC4: reachability plan emitted (dry-run)"
    return
  fi
  _code=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 20 "$E2E_APP_URL" || echo 000)
  case "$_code" in
    2*|3*) ok "AC4: app reachable at $E2E_APP_URL (HTTP $_code)" ;;
    *) bad "AC4: app not reachable at $E2E_APP_URL (HTTP $_code)" ;;
  esac
}

# --- main ----------------------------------------------------------------------------------

main() {
  require_env
  if [ "$DRY_RUN" = "1" ]; then
    log "DRY RUN — printing the verification plan; no servers are touched, no secrets are revealed."
  else
    log "LIVE RUN against $PLORIGO_CP_URL — this prepares real VPSes."
  fi
  step_manual
  step_managed
  step_idempotent
  step_reach
  printf '\n'
  log "Verification summary: ${PASS_COUNT} passed, ${FAIL_COUNT} failed."
  [ "$FAIL_COUNT" -eq 0 ] || die "verification gate FAILED — see failures above and docs/verification/fresh-vps-e2e.md"
  log "Record the exact VPS images, commands, and any limitations in the PR (AC7)."
}

main
