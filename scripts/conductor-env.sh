#!/usr/bin/env bash
# Shared environment for the Conductor lifecycle scripts (setup, run, archive).
#
# This file is *sourced*, never executed: it must not change the caller's shell
# options (no `set -e`), must not `cd`, and must not rely on `$0`/`dirname "$0"`
# (those refer to the sourcing script, not this file). It assumes the caller has
# already `cd`'d to the repository root so `deploy/docker-compose.yml` resolves.
#
# Each Conductor workspace gets its own isolated Docker stack so workspaces never
# collide and can run concurrently. The isolation key is the Docker Compose
# project name, derived once here so setup/run/archive all target the SAME
# project (otherwise archive would tear down the wrong container/volume):
#
#   project : plorigo-<sanitized-workspace-name>
#   ports   : API = CONDUCTOR_PORT, web = +1, Postgres host = +2

# Derive a stable workspace token. CONDUCTOR_WORKSPACE_NAME is provided to all
# three lifecycle scripts; fall back to the worktree directory name so the value
# is identical across setup/run/archive even if the variable is ever absent.
ws="${CONDUCTOR_WORKSPACE_NAME:-}"
if [ -z "$ws" ]; then
  ws="$(basename "${CONDUCTOR_ROOT_PATH:-$PWD}")"
fi

# Sanitize into a valid Compose project token: lowercase, only [a-z0-9_-],
# collapsed dashes, no leading/trailing dash. The literal "plorigo-" prefix
# below then guarantees a valid leading character.
ws="$(printf '%s' "$ws" \
  | tr '[:upper:]' '[:lower:]' \
  | tr -c 'a-z0-9_-' '-' \
  | tr -s '-' \
  | sed 's/^-*//; s/-*$//')"
[ -n "$ws" ] || ws="default"

export PLORIGO_PROJECT="plorigo-${ws}"

# Conductor allocates a 10-port range starting at CONDUCTOR_PORT (default 8080
# when run outside Conductor): API on the first, dashboard on +1, and the
# workspace's published Postgres host port on +2.
base_port="${CONDUCTOR_PORT:-8080}"
export PLORIGO_API_PORT="$base_port"
export PLORIGO_WEB_PORT="$((base_port + 1))"
export PLORIGO_PG_PORT="$((base_port + 2))"

# Required by every compose call: the compose file interpolates ${APP_MASTER_KEY:?}
# on the controlplane service at parse time, so even `up -d postgres` / `down`
# fail when it is unset.
export APP_MASTER_KEY="${APP_MASTER_KEY:-conductor-local-dev-key}"

# Always point at this workspace's own Postgres (host port +2), overriding any
# inherited DATABASE_URL so the connection string can never drift from the
# container the scripts actually start.
export DATABASE_URL="postgres://plorigo:plorigo@localhost:${PLORIGO_PG_PORT}/plorigo?sslmode=disable"

# Run docker compose against THIS workspace's isolated project. `-p` overrides the
# compose file's top-level `name:`, giving per-workspace containers, volumes, and
# network. Requires the caller's CWD to be the repository root.
plorigo_compose() {
  docker compose -p "$PLORIGO_PROJECT" -f deploy/docker-compose.yml "$@"
}

# Log the computed identity so every script prints the same line — a cheap way to
# confirm setup/run/archive agree on the project they manage.
echo "Conductor workspace: project=${PLORIGO_PROJECT} api=${PLORIGO_API_PORT} web=${PLORIGO_WEB_PORT} pg=${PLORIGO_PG_PORT}"
