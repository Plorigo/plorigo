#!/usr/bin/env bash
# Regenerate all code: protobuf (Go + TS clients) and the sqlc DB package.
set -euo pipefail
cd "$(dirname "$0")/.."

(cd proto && go tool buf generate)
go tool sqlc generate
