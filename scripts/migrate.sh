#!/usr/bin/env bash
# Apply database migrations with goose.
set -euo pipefail
cd "$(dirname "$0")/.."

: "${DATABASE_URL:=postgres://plorigo:plorigo@localhost:5432/plorigo?sslmode=disable}"
go tool goose -dir migrations postgres "$DATABASE_URL" up
