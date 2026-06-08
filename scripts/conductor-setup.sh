#!/usr/bin/env bash
# Conductor setup: runs once when a workspace is created. Installs the toolchain
# and generates code. The database is owned by the run script
# (scripts/conductor-run.sh), which brings up and migrates this workspace's
# isolated Postgres every time you click Run — so setup stays fast, deterministic,
# and free of any Docker dependency.
set -euo pipefail
cd "$(dirname "$0")/.."

make setup
make generate
