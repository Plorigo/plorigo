#!/usr/bin/env bash
# Conductor setup: runs once when a workspace is created. Installs the toolchain,
# generates code, and bootstraps this workspace's own isolated Postgres so the
# stack is ready to run.
set -euo pipefail
cd "$(dirname "$0")/.."

# Mandatory: dependencies and generated code. These must fail loudly — a broken
# install or codegen is a real problem, not something to skip.
make setup
make generate

# Best-effort: bring up and migrate this workspace's isolated Postgres. Guarded so
# a missing/stopped Docker daemon (or a cloud workspace) never fails workspace
# creation — the run script brings the DB up again idempotently when you click Run.
# shellcheck source=scripts/conductor-env.sh
source ./scripts/conductor-env.sh

if [ "${CONDUCTOR_IS_LOCAL:-1}" != "1" ]; then
  echo "conductor-setup: cloud workspace; skipping local Postgres bootstrap."
elif ! command -v docker >/dev/null 2>&1; then
  echo "conductor-setup: docker not found; skipping local Postgres bootstrap."
elif ! docker info >/dev/null 2>&1; then
  echo "conductor-setup: docker daemon unreachable; skipping local Postgres bootstrap."
elif plorigo_compose up -d --wait postgres && ./scripts/migrate.sh; then
  echo "conductor-setup: Postgres ready and migrated (${PLORIGO_PROJECT}, host port ${PLORIGO_PG_PORT})."
else
  echo "conductor-setup: Postgres bootstrap failed; continuing (Run will retry)." >&2
fi
