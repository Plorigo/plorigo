#!/usr/bin/env bash
# Run the local Conductor dev stack: this workspace's own isolated Postgres, the
# migrated control plane, and the Vite dashboard. Each workspace gets its own
# Docker project, database, and host ports (see scripts/conductor-env.sh) so
# multiple workspaces can run at once without colliding.
set -euo pipefail
cd "$(dirname "$0")/.."
root="$(pwd)"

# Per-workspace project, ports, APP_MASTER_KEY, DATABASE_URL, and plorigo_compose().
# shellcheck source=scripts/conductor-env.sh
source ./scripts/conductor-env.sh

export PORT="$PLORIGO_API_PORT"
export PLORIGO_ENV=dev
export PLORIGO_BASE_URL="http://localhost:${PLORIGO_WEB_PORT}"
export VITE_CONTROLPLANE_URL="http://localhost:${PLORIGO_API_PORT}"

# Bring up this workspace's Postgres and wait until it is healthy before migrating.
plorigo_compose up -d --wait postgres
./scripts/migrate.sh

echo "Plorigo dashboard: http://localhost:${PLORIGO_WEB_PORT}"
echo "Plorigo control plane: http://localhost:${PLORIGO_API_PORT}"

exec pnpm --dir apps/web exec concurrently \
  --kill-others \
  --kill-others-on-fail \
  --names controlplane,web \
  --prefix-colors blue,green \
  "cd \"$root\" && go run ./cmd/controlplane" \
  "cd \"$root/apps/web\" && pnpm exec vite --host 127.0.0.1 --port \"$PLORIGO_WEB_PORT\""
