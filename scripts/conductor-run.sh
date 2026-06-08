#!/usr/bin/env bash
# Run the local Conductor dev stack: shared Postgres, migrated control plane, and
# the Vite dashboard. Conductor allocates a 10-port range per workspace; use the
# first port for the API and the next port for the dashboard.
set -euo pipefail
cd "$(dirname "$0")/.."

base_port="${CONDUCTOR_PORT:-8080}"
web_port="$((base_port + 1))"
root="$(pwd)"

export APP_MASTER_KEY="${APP_MASTER_KEY:-conductor-local-dev-key}"
export DATABASE_URL="${DATABASE_URL:-postgres://plorigo:plorigo@localhost:5432/plorigo?sslmode=disable}"
export PORT="$base_port"
export PLORIGO_ENV=dev
export PLORIGO_BASE_URL="http://localhost:${web_port}"
export VITE_CONTROLPLANE_URL="http://localhost:${base_port}"

APP_MASTER_KEY="$APP_MASTER_KEY" docker compose -f deploy/docker-compose.yml up -d postgres
./scripts/migrate.sh

echo "Plorigo dashboard: http://localhost:${web_port}"
echo "Plorigo control plane: http://localhost:${base_port}"

exec pnpm --dir apps/web exec concurrently \
  --kill-others \
  --kill-others-on-fail \
  --names controlplane,web \
  --prefix-colors blue,green \
  "cd \"$root\" && go run ./cmd/controlplane" \
  "cd \"$root/apps/web\" && pnpm exec vite --host 127.0.0.1 --port \"$web_port\""
