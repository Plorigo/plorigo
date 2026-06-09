#!/usr/bin/env bash
# Start Postgres, apply migrations, and run the control plane. Run the dashboard
# separately with `pnpm --dir apps/web dev`.
set -euo pipefail
cd "$(dirname "$0")/.."

docker compose -f deploy/docker-compose.yml up -d postgres
./scripts/migrate.sh
export PLORIGO_ENV="${PLORIGO_ENV:-dev}"
export PLORIGO_PUBLIC_URL="${PLORIGO_PUBLIC_URL:-http://localhost:${PORT:-8080}}"
exec go run ./cmd/controlplane
