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
# The agent connects to the control plane's API origin (not the dashboard), so the
# install command shown in the dashboard must use this, not PLORIGO_BASE_URL.
export PLORIGO_PUBLIC_URL="http://localhost:${PLORIGO_API_PORT}"
export VITE_CONTROLPLANE_URL="http://localhost:${PLORIGO_API_PORT}"

# Run is the single owner of the DB lifecycle: setup no longer touches Postgres, so
# bring up this workspace's database here (idempotent) and wait until it is healthy
# before migrating. This must happen on every Run — the container does not survive a
# reboot/sleep, and new migrations land between runs.
plorigo_compose up -d --wait postgres
./scripts/migrate.sh

# Create/refresh the dev login user so the dashboard is immediately usable. Idempotent
# (resets the password to the documented default); best-effort so a seed hiccup never
# blocks the stack from starting. make seed reuses the DATABASE_URL/APP_MASTER_KEY this
# script already exported, so it targets this workspace's database.
if ! make seed; then
  echo "warning: could not seed dev login user; retry later with 'make seed'" >&2
fi

echo "Plorigo dashboard: http://localhost:${PLORIGO_WEB_PORT}"
echo "Plorigo control plane: http://localhost:${PLORIGO_API_PORT}"
echo "Dev login: dev@plorigo.local / devpassword (override via make seed SEED_EMAIL=… SEED_PASSWORD=…)"

exec pnpm --dir apps/web exec concurrently \
  --kill-others \
  --kill-others-on-fail \
  --names controlplane,web \
  --prefix-colors blue,green \
  "cd \"$root\" && go run ./cmd/controlplane" \
  "cd \"$root/apps/web\" && pnpm exec vite --host 127.0.0.1 --port \"$PLORIGO_WEB_PORT\""
