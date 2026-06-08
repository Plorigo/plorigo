#!/usr/bin/env bash
# Conductor archive: runs before a workspace is archived. Tears down this
# workspace's isolated Docker stack (container + volume + network) and deletes
# regenerable, git-ignored dependencies and build artifacts to reclaim disk.
#
# Archiving is DESTRUCTIVE to this workspace's local database data — the Postgres
# volume is removed. Everything here is regenerable via `make setup`/`make generate`.
#
# This must NEVER block archiving, so it does not `set -e`: each step is best-effort
# with a logged reason, and the script always exits 0. (We deliberately do not
# abort on Docker/filesystem errors here, but we log every one rather than swallow
# it silently — see docs/conventions.md on privileged operations.)
set -uo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/conductor-env.sh
source ./scripts/conductor-env.sh

# --- Tear down this workspace's Docker stack (detect-then-skip, never abort) ---
if ! command -v docker >/dev/null 2>&1; then
  echo "conductor-archive: docker not found; skipping container/volume teardown."
elif ! docker info >/dev/null 2>&1; then
  echo "conductor-archive: docker daemon unreachable; skipping container/volume teardown."
else
  echo "conductor-archive: removing Docker project ${PLORIGO_PROJECT} (containers, volumes, network)."
  # --profile storage so an ever-started minio is included; -v drops named volumes
  # (the Postgres data); --remove-orphans cleans stragglers. APP_MASTER_KEY is
  # exported by conductor-env.sh, which compose needs even to parse the file for `down`.
  if ! plorigo_compose --profile storage down -v --remove-orphans; then
    echo "conductor-archive: 'compose down' failed; continuing." >&2
  fi
fi

# --- Reclaim disk: delete only regenerable, git-ignored artifacts ---
# Guard against a wrong CWD before any rm -rf. Normal case: we're at the git
# worktree root (where we cd'd); fall back to sentinel files if the path compare is
# thrown off (e.g. symlinks). Never touch committed code (internal/platform/database/db,
# migrations, db/query, proto sources).
at_repo_root() {
  local top
  top="$(git rev-parse --show-toplevel 2>/dev/null || true)"
  if [ -n "$top" ] && [ "$top" = "$(pwd -P)" ]; then
    return 0
  fi
  [ -f conductor.json ] && [ -f deploy/docker-compose.yml ]
}

if at_repo_root; then
  regenerable=(
    node_modules
    apps/web/node_modules
    apps/web/dist
    apps/web/src/gen
    proto/gen
    internal/platform/web/dist
    bin
    dist
    build
    .vite
    .turbo
    .cache
    .eslintcache
  )
  for path in "${regenerable[@]}"; do
    [ -e "$path" ] || continue
    if rm -rf -- "$path"; then
      echo "conductor-archive: removed $path"
    else
      echo "conductor-archive: failed to remove $path; continuing." >&2
    fi
  done
else
  echo "conductor-archive: cannot confirm repository root; skipping file cleanup." >&2
fi

echo "conductor-archive: done."
exit 0
