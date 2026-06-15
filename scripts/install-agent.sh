#!/bin/sh
# Plorigo server-agent installer. Prepares a fresh Ubuntu LTS server for Plorigo and starts
# the agent, which registers with your control plane and reports the server online:
#
#   curl -fsSL https://raw.githubusercontent.com/Plorigo/plorigo/main/scripts/install-agent.sh \
#     | sudo sh -s -- --control-plane https://cp.example.com --token <one-time-token>
#
# On a fresh box it installs and verifies the prerequisites the agent needs — Docker Engine
# (with BuildKit/buildx) and the Caddy binary — creates the data directory, places the agent
# binary, installs the systemd service, and verifies Docker access, ports 80/443, and outbound
# control-plane connectivity before reporting success. It is idempotent: safe to re-run for
# reconnect, repair, or token rotation (re-running rewrites the unit so a fresh token takes
# effect). The one-time token is shown once in the dashboard ("Connect server"); it is used
# only on the first start, after which the agent stores a durable credential under --data-dir.
# See docs/architecture/agent.md and docs/architecture/server-management.md.
#
# Supported OS: Ubuntu 22.04 / 24.04 LTS. Must run as root (it installs system packages).
#
# Package sources (auditable): Docker Engine from Docker's official apt repository
#   (key https://download.docker.com/linux/ubuntu/gpg, repo https://download.docker.com/linux/ubuntu)
# and Caddy from Caddy's official apt repository
#   (key https://dl.cloudsmith.io/public/caddy/stable/gpg.key, repo https://dl.cloudsmith.io/public/caddy/stable/deb/debian).
#
# Flags:
#   --control-plane <url>   control-plane base URL the agent connects to (required)
#   --token <token>         one-time registration token (required)
#   --data-dir <path>       agent data directory (default /var/lib/plorigo-agent)
#   --binary-url <url>      use this prebuilt agent binary (http(s)/file URL) instead of the
#                           published release; otherwise the agent is downloaded from the
#                           project's GitHub Releases (or built from a source checkout)
#   --skip-prep             skip host preparation (OS check, apt, Docker/Caddy install, port
#                           checks); only install the agent + service. For technical users
#                           whose box already has Docker and Caddy, and for the agent e2e.
#   --skip-os-check         do not reject non-Ubuntu / non-LTS hosts (best-effort)
#
# Env knobs: PLORIGO_AGENT_VERSION pins a release tag (default: latest);
# PLORIGO_INSTALL_SKIP_VERIFY=1 bypasses the binary checksum check (NOT recommended).
#
# Supply chain: the agent is downloaded over HTTPS from GitHub Releases and its sha256 is
# verified against the release's signed checksums.txt before it runs; release binaries also
# carry SLSA build-provenance attestations (verify with `gh attestation verify`). This script
# never prints the registration token and never enables shell tracing (`set -x`), so the token
# cannot leak into logs.
set -eu

CONTROL_PLANE=""
TOKEN=""
DATA_DIR="/var/lib/plorigo-agent"
# --binary-url / PLORIGO_AGENT_BINARY_URL overrides the source of the agent binary. When
# unset (the normal case) the agent is downloaded from GitHub Releases and checksum-verified.
AGENT_BINARY_URL="${PLORIGO_AGENT_BINARY_URL:-}"
SKIP_PREP="${PLORIGO_INSTALL_SKIP_PREP:-0}"
SKIP_OS_CHECK="${PLORIGO_INSTALL_SKIP_OS_CHECK:-0}"
# Release source for the prebuilt agent. AGENT_VERSION pins a tag (default: latest release).
# SKIP_VERIFY disables the checksum check — only for local/offline testing, never in prod.
RELEASE_REPO="${PLORIGO_RELEASE_REPO:-Plorigo/plorigo}"
AGENT_VERSION="${PLORIGO_AGENT_VERSION:-latest}"
SKIP_VERIFY="${PLORIGO_INSTALL_SKIP_VERIFY:-0}"

# Privileged paths. These default to the standard system locations and are overridable only
# to make the script testable without root (see internal/app/install_agent_shim_test.go); in
# production the defaults are always used.
BIN_PATH="${PLORIGO_AGENT_BIN_PATH:-/usr/local/bin/plorigo-agent}"
SYSTEMD_DIR="${PLORIGO_SYSTEMD_DIR:-/etc/systemd/system}"
APT_KEYRINGS_DIR="${PLORIGO_APT_KEYRINGS_DIR:-/etc/apt/keyrings}"
APT_SOURCES_DIR="${PLORIGO_APT_SOURCES_DIR:-/etc/apt/sources.list.d}"
OS_RELEASE="${PLORIGO_OS_RELEASE:-/etc/os-release}"

ARCH="amd64"
CODENAME=""
OS_NAME=""
OS_VER=""
umask 022

# ---------------------------------------------------------------------------------------------
# Logging, error reporting, and secret redaction.
# ---------------------------------------------------------------------------------------------

log() { printf '%s\n' "$1"; }
warn() { printf 'Warning: %s\n' "$1" >&2; }

# fail prints a plain-English error plus a recovery hint (principles.md) and exits non-zero.
fail() {
  printf 'Error: %s\n' "$1" >&2
  if [ "${2:-}" != "" ]; then
    printf 'Recovery: %s\n' "$2" >&2
  fi
  exit 1
}

# redact filters stdin, replacing the registration token with a placeholder so command output
# (e.g. a journal tail on startup failure) can never leak it. No-op when the token is empty.
redact() {
  if [ -n "${TOKEN:-}" ]; then
    esc=$(printf '%s' "$TOKEN" | sed 's/[\\/&]/\\&/g')
    sed "s/$esc/***REDACTED***/g"
  else
    cat
  fi
}

# ---------------------------------------------------------------------------------------------
# Argument parsing and validation.
# ---------------------------------------------------------------------------------------------

while [ $# -gt 0 ]; do
  case "$1" in
    --control-plane) CONTROL_PLANE="$2"; shift 2 ;;
    --token) TOKEN="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --binary-url) AGENT_BINARY_URL="$2"; shift 2 ;;
    --skip-prep) SKIP_PREP=1; shift ;;
    --skip-os-check) SKIP_OS_CHECK=1; shift ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

if [ -z "$CONTROL_PLANE" ] || [ -z "$TOKEN" ]; then
  echo "usage: install-agent.sh --control-plane <url> --token <token>" >&2
  exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
  SYSTEMD=1
else
  SYSTEMD=0
fi

# ---------------------------------------------------------------------------------------------
# Steps.
# ---------------------------------------------------------------------------------------------

require_root() {
  if [ "$(id -u)" != "0" ]; then
    fail "this installer must run as root to prepare the server and install system packages." \
         "Re-run with sudo, e.g.  curl -fsSL <installer-url> | sudo sh -s -- --control-plane $CONTROL_PLANE --token <token>"
  fi
}

# check_connectivity verifies the control plane is reachable from this server. It branches on
# curl's exit code, not the HTTP status: the control plane has no health endpoint, so a bare
# GET may answer 404/415 — that still proves reachability. Only connection-level failures
# (DNS, refused, timeout) mean "unreachable".
check_connectivity() {
  log "Checking control-plane connectivity ($CONTROL_PLANE)…"
  code=0
  curl -sS -o /dev/null --max-time 10 "$CONTROL_PLANE" >/dev/null 2>&1 || code=$?
  if [ "$code" = "0" ]; then
    log "Control plane is reachable."
    return 0
  fi
  case "$code" in
    6)  fail "cannot resolve the control-plane host in $CONTROL_PLANE." \
             "Check the URL and this server's DNS, then re-run." ;;
    28) fail "timed out reaching the control plane at $CONTROL_PLANE." \
             "Check outbound network / firewall / proxy and that the control plane is up, then re-run." ;;
    *)  fail "cannot reach the control plane at $CONTROL_PLANE (curl exit $code)." \
             "Confirm the URL, that outbound HTTPS is allowed, and the control plane is up, then re-run." ;;
  esac
}

detect_os() {
  if [ ! -r "$OS_RELEASE" ]; then
    fail "cannot read $OS_RELEASE to detect the operating system." \
         "Plorigo supports Ubuntu 22.04 / 24.04 LTS. Run on a supported OS, or pass --skip-prep to install only the agent."
  fi
  # os-release is a trusted, simple KEY=value file.
  # shellcheck disable=SC1090
  . "$OS_RELEASE"
  OS_NAME="${NAME:-${ID:-unknown}}"
  OS_VER="${VERSION_ID:-}"
  CODENAME="${UBUNTU_CODENAME:-}"
  ARCH=$(dpkg --print-architecture 2>/dev/null || echo amd64)

  case "${ID:-}:$OS_VER" in
    ubuntu:22.04 | ubuntu:24.04)
      log "Detected supported OS: $OS_NAME $OS_VER ($ARCH)." ;;
    *)
      if [ "$SKIP_OS_CHECK" = "1" ]; then
        warn "unsupported OS ($OS_NAME $OS_VER); continuing because --skip-os-check was given."
      else
        fail "unsupported OS: $OS_NAME $OS_VER. Plorigo's installer supports Ubuntu 22.04 and 24.04 LTS." \
             "Use a supported OS, re-run with --skip-os-check to try anyway, or install the agent manually (docs/architecture/agent.md)."
      fi ;;
  esac

  if [ -z "$CODENAME" ]; then
    case "$OS_VER" in
      22.04) CODENAME=jammy ;;
      24.04) CODENAME=noble ;;
    esac
  fi
}

# run_apt runs apt-get with a bounded lock wait and turns failures into plain-English errors.
# It distinguishes a held dpkg/apt lock (very common on a freshly booted cloud box running
# unattended-upgrades) from other failures, and routes output through redact() to be safe.
run_apt() {
  desc="$1"; shift
  out=$(DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a NEEDRESTART_SUSPEND=1 \
        apt-get -o DPkg::Lock::Timeout=120 "$@" 2>&1) || {
    printf '%s\n' "$out" | redact >&2
    if printf '%s' "$out" | grep -qi 'could not get lock\|lock.*frontend\|temporarily unavailable\|is another process using it'; then
      fail "could not $desc: the apt/dpkg lock is held by another process (often unattended-upgrades on a fresh server)." \
           "Wait for it to finish (or 'sudo systemctl stop unattended-upgrades'), then re-run — it is safe to re-run."
    fi
    fail "could not $desc." "Check the apt output above and this server's network/repository access, then re-run."
  }
}

prepare_apt() {
  log "Updating the package index and installing base prerequisites…"
  run_apt "update the package index" update
  run_apt "install base prerequisites" install -y ca-certificates curl
}

install_keyring() {
  name="$1"; url="$2"
  mkdir -p "$APT_KEYRINGS_DIR"
  curl -fsSL "$url" -o "$APT_KEYRINGS_DIR/$name" \
    || fail "could not download the signing key from $url." \
            "Check this server's network access to the package repositories, then re-run."
  chmod a+r "$APT_KEYRINGS_DIR/$name"
}

write_source() {
  name="$1"; line="$2"
  mkdir -p "$APT_SOURCES_DIR"
  printf '%s\n' "$line" > "$APT_SOURCES_DIR/$name"
}

ensure_docker() {
  if command -v docker >/dev/null 2>&1 && docker buildx version >/dev/null 2>&1; then
    log "Docker Engine with BuildKit is already installed."
  else
    log "Installing Docker Engine…"
    install_keyring "docker.asc" "https://download.docker.com/linux/ubuntu/gpg"
    write_source "docker.list" \
      "deb [arch=$ARCH signed-by=$APT_KEYRINGS_DIR/docker.asc] https://download.docker.com/linux/ubuntu $CODENAME stable"
    run_apt "refresh the Docker package index" update
    run_apt "install Docker Engine" install -y \
      docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  fi
  if [ "$SYSTEMD" = "1" ]; then
    systemctl enable --now docker >/dev/null 2>&1 \
      || warn "could not enable the docker service; verifying daemon access anyway."
  fi
  docker info >/dev/null 2>&1 \
    || fail "the Docker daemon is not reachable after installation." \
            "Check 'systemctl status docker' and 'journalctl -u docker', then re-run."
  log "Docker daemon is reachable."
}

ensure_caddy() {
  if command -v caddy >/dev/null 2>&1 && caddy version >/dev/null 2>&1; then
    log "Caddy is already installed."
  else
    log "Installing Caddy…"
    install_keyring "caddy-stable.asc" "https://dl.cloudsmith.io/public/caddy/stable/gpg.key"
    write_source "caddy-stable.list" \
      "deb [signed-by=$APT_KEYRINGS_DIR/caddy-stable.asc] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
    run_apt "refresh the Caddy package index" update
    run_apt "install Caddy" install -y caddy
  fi
  # The Plorigo agent runs and configures Caddy itself — its own Caddyfile, admin API on
  # localhost:2019, auto_https off, listening on :80 (see internal/agentcore/caddy.go). The
  # packaged caddy.service would bind :80/:443 and collide, so disable it; the agent owns the
  # Caddy process.
  if [ "$SYSTEMD" = "1" ] && systemctl list-unit-files 2>/dev/null | grep -q '^caddy\.service'; then
    systemctl disable --now caddy >/dev/null 2>&1 \
      || warn "could not disable the packaged caddy.service; ensure it is not holding ports 80/443/2019 (the Plorigo agent runs Caddy itself)."
    log "Disabled the packaged caddy.service (the Plorigo agent runs Caddy itself)."
  fi
}

ensure_dirs() {
  install -d -m 0700 "$DATA_DIR" \
    || fail "could not create the agent data directory $DATA_DIR." "Check filesystem permissions, then re-run."
}

# host_arch maps the machine to the architecture used in release asset names (Go/dpkg style).
host_arch() {
  a=$(dpkg --print-architecture 2>/dev/null || true)
  if [ -z "$a" ]; then
    case "$(uname -m 2>/dev/null)" in
      x86_64 | amd64) a=amd64 ;;
      aarch64 | arm64) a=arm64 ;;
      *) a=$(uname -m 2>/dev/null || echo amd64) ;;
    esac
  fi
  printf '%s' "$a"
}

# agent_release_base is the GitHub Releases download base for the pinned (or latest) version.
agent_release_base() {
  if [ "$AGENT_VERSION" = "latest" ]; then
    printf 'https://github.com/%s/releases/latest/download' "$RELEASE_REPO"
  else
    printf 'https://github.com/%s/releases/download/%s' "$RELEASE_REPO" "$AGENT_VERSION"
  fi
}

# sha256_of prints the sha256 of a file, using whichever tool is present (Ubuntu ships
# sha256sum; shasum is the fallback). Returns non-zero if neither exists.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    return 1
  fi
}

# verify_release_checksum fails (and removes $file) unless $file's sha256 matches the entry
# for $asset in the release checksums.txt. Running a root binary we could not verify is a
# security risk, so a missing/mismatched checksum is fatal unless SKIP_VERIFY is set.
verify_release_checksum() {
  file="$1"; asset="$2"; sums_url="$3"
  if [ "$SKIP_VERIFY" = "1" ]; then
    warn "skipping agent binary checksum verification (PLORIGO_INSTALL_SKIP_VERIFY=1)."
    return 0
  fi
  sums=$(mktemp) || { rm -f "$file"; fail "could not create a temp file to verify the agent binary." "Check disk space, then re-run."; }
  if ! curl -fsSL "$sums_url" -o "$sums" 2>/dev/null; then
    rm -f "$file" "$sums"
    fail "could not download the release checksums ($sums_url) to verify the agent binary." \
         "Re-run; if the release lacks checksums, set PLORIGO_INSTALL_SKIP_VERIFY=1 to bypass (not recommended)."
  fi
  expected=$(awk -v a="$asset" '{n=$2; sub(/^\*/, "", n); if (n == a) { print $1; exit }}' "$sums")
  rm -f "$sums"
  if [ -z "$expected" ]; then
    rm -f "$file"
    fail "no checksum for $asset in the release checksums." \
         "The release may be incomplete; re-run later, or set PLORIGO_INSTALL_SKIP_VERIFY=1 (not recommended)."
  fi
  actual=$(sha256_of "$file") || { rm -f "$file"; fail "no sha256 tool (sha256sum/shasum) to verify the agent binary." "Install coreutils, then re-run."; }
  if [ "$expected" != "$actual" ]; then
    rm -f "$file"
    fail "agent binary checksum mismatch for $asset (expected $expected, got $actual)." \
         "Do not trust this download. Re-run; if it persists, report it — the release asset may be corrupted or tampered with."
  fi
  log "Agent binary checksum verified."
}

acquire_binary() {
  bindir=$(dirname "$BIN_PATH")
  mkdir -p "$bindir"
  tmp=$(mktemp "$bindir/.plorigo-agent.XXXXXX") \
    || fail "could not create a temporary file for the agent binary in $bindir." "Check permissions and disk space, then re-run."
  if [ -n "$AGENT_BINARY_URL" ]; then
    log "Downloading the agent binary…"
    curl -fsSL "$AGENT_BINARY_URL" -o "$tmp" \
      || { rm -f "$tmp"; fail "could not download the agent binary." "Check --binary-url / PLORIGO_AGENT_BINARY_URL and network access, then re-run."; }
  elif command -v go >/dev/null 2>&1 && [ -f go.mod ]; then
    log "Building the agent from source…"
    go build -o "$tmp" ./cmd/agent \
      || { rm -f "$tmp"; fail "could not build the agent from source." "Check the Go toolchain and that you are in the repo root, then re-run."; }
  else
    asset="plorigo-agent-linux-$(host_arch)"
    base=$(agent_release_base)
    log "Downloading the agent binary ($base/$asset)…"
    curl -fsSL "$base/$asset" -o "$tmp" \
      || { rm -f "$tmp"; fail "could not download the agent binary from $base/$asset." "Check this server's network access to github.com, or pass --binary-url to a prebuilt binary, then re-run."; }
    verify_release_checksum "$tmp" "$asset" "$base/checksums.txt"
  fi
  chmod +x "$tmp" || { rm -f "$tmp"; fail "could not mark the agent binary executable." "Check filesystem permissions, then re-run."; }
  mv -f "$tmp" "$BIN_PATH" || { rm -f "$tmp"; fail "could not install the agent binary to $BIN_PATH." "Check permissions on $bindir, then re-run."; }
  log "Agent binary installed at $BIN_PATH."
}

# listeners_on prints the `ss` listening rows whose local socket ends in :<port>.
listeners_on() {
  ss -ltnpH 2>/dev/null | awk -v p=":$1" 'substr($4, length($4) - length(p) + 1) == p'
}

# check_port verifies a port the agent's Caddy needs is free, tolerating the case where our
# own Caddy already holds it (a legitimate re-run). severity "fatal" stops install; "warn"
# only warns.
check_port() {
  port="$1"; severity="$2"
  rows=$(listeners_on "$port") || rows=""
  if [ -z "$rows" ]; then
    log "Port $port is free."
    return 0
  fi
  if printf '%s' "$rows" | grep -q 'caddy'; then
    log "Port $port is held by Plorigo's own Caddy (expected on re-run)."
    return 0
  fi
  if [ "$severity" = "fatal" ]; then
    fail "port $port is already in use by another service. Plorigo runs Caddy on port $port and needs it free." \
         "Stop the conflicting service (e.g. 'systemctl stop nginx' or 'systemctl stop apache2'), then re-run."
  fi
  warn "port $port is in use by another service; Plorigo does not bind it yet, but custom-domain HTTPS will need it free."
}

dump_agent_log() {
  warn "Recent agent logs (secrets redacted):"
  journalctl -u plorigo-agent --no-pager -n 30 2>/dev/null | redact >&2 || true
}

verify_agent_started() {
  i=0
  while [ "$i" -lt 10 ]; do
    if systemctl is-active --quiet plorigo-agent; then
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  dump_agent_log
  fail "the Plorigo agent service did not become active." \
       "See the (redacted) log above; check that Docker is running and the registration token is valid (mint a fresh one in the dashboard), then re-run."
}

install_service() {
  unit="$SYSTEMD_DIR/plorigo-agent.service"
  mkdir -p "$SYSTEMD_DIR"
  # Write atomically with 0600 (the unit carries the token in Environment=).
  tmp=$(mktemp "$SYSTEMD_DIR/.plorigo-agent.XXXXXX") \
    || fail "could not create the systemd unit in $SYSTEMD_DIR." "Check permissions, then re-run."
  cat > "$tmp" <<EOF
[Unit]
Description=Plorigo server agent
After=network-online.target docker.service
Wants=network-online.target docker.service

[Service]
Environment=PLORIGO_CONTROL_PLANE=$CONTROL_PLANE
Environment=PLORIGO_AGENT_TOKEN=$TOKEN
Environment=PLORIGO_AGENT_DATA_DIR=$DATA_DIR
ExecStart=$BIN_PATH
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  chmod 0600 "$tmp"
  mv -f "$tmp" "$unit" || { rm -f "$tmp"; fail "could not install the systemd unit at $unit." "Check permissions, then re-run."; }

  systemctl daemon-reload \
    || fail "could not reload systemd." "Run 'systemctl daemon-reload' and check systemd, then re-run."
  systemctl reset-failed plorigo-agent >/dev/null 2>&1 || true
  systemctl enable --now plorigo-agent \
    || { dump_agent_log; fail "could not start the Plorigo agent service." \
            "See the (redacted) log above; check Docker is running and the token is valid, then re-run."; }
  verify_agent_started
  log "Plorigo agent installed and started. Check: systemctl status plorigo-agent"
}

start_agent() {
  if [ "$SYSTEMD" = "1" ]; then
    install_service
  else
    # No systemd (e.g. a minimal container): run the agent in the foreground. All verification
    # above has already run, so this only replaces the process with the agent itself.
    log "systemd is not available; starting the agent in the foreground."
    exec "$BIN_PATH" --control-plane "$CONTROL_PLANE" --token "$TOKEN" --data-dir "$DATA_DIR"
  fi
}

# ---------------------------------------------------------------------------------------------
# Main.
# ---------------------------------------------------------------------------------------------

require_root
check_connectivity

if [ "$SKIP_PREP" = "1" ]; then
  warn "--skip-prep set: skipping OS check and Docker/Caddy preparation; assuming this host is already prepared."
else
  detect_os
  prepare_apt
  ensure_docker
  ensure_caddy
fi

ensure_dirs
acquire_binary

if [ "$SKIP_PREP" != "1" ]; then
  check_port 80 fatal
  check_port 443 warn
fi

start_agent
