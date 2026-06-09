#!/bin/sh
# Plorigo server-agent installer. Installs the agent binary and a systemd service that
# registers with your control plane and reports the server online. Run as root on a
# Linux host (with Docker, for later deploy steps):
#
#   curl -fsSL https://raw.githubusercontent.com/Plorigo/plorigo/main/scripts/install-agent.sh \
#     | sh -s -- --control-plane https://cp.example.com --token <one-time-token>
#
# The one-time token is shown once in the dashboard ("Connect server"). It is used only on
# the first start; the agent then stores a durable credential under --data-dir. See
# docs/architecture/agent.md.
set -eu

CONTROL_PLANE=""
TOKEN=""
DATA_DIR="/var/lib/plorigo-agent"
BIN_PATH="/usr/local/bin/plorigo-agent"
# Set PLORIGO_AGENT_BINARY_URL (or --binary-url) to a prebuilt agent binary for this
# platform. Prebuilt release artifacts are coming; see ROADMAP.md.
AGENT_BINARY_URL="${PLORIGO_AGENT_BINARY_URL:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --control-plane) CONTROL_PLANE="$2"; shift 2 ;;
    --token) TOKEN="$2"; shift 2 ;;
    --data-dir) DATA_DIR="$2"; shift 2 ;;
    --binary-url) AGENT_BINARY_URL="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

if [ -z "$CONTROL_PLANE" ] || [ -z "$TOKEN" ]; then
  echo "usage: install-agent.sh --control-plane <url> --token <token>" >&2
  exit 1
fi

# 1. Obtain the agent binary.
if [ -n "$AGENT_BINARY_URL" ]; then
  echo "Downloading agent from $AGENT_BINARY_URL"
  curl -fsSL "$AGENT_BINARY_URL" -o "$BIN_PATH"
  chmod +x "$BIN_PATH"
elif command -v go >/dev/null 2>&1 && [ -f go.mod ]; then
  echo "Building agent from source"
  go build -o "$BIN_PATH" ./cmd/agent
else
  echo "No agent binary available. Set PLORIGO_AGENT_BINARY_URL (or --binary-url) to a" >&2
  echo "prebuilt agent binary for this platform, or run from a source checkout with Go" >&2
  echo "installed. See docs/architecture/agent.md and ROADMAP.md." >&2
  exit 1
fi

# 2. Install and start as a systemd service (root), or run in the foreground otherwise.
if [ "$(id -u)" = "0" ] && command -v systemctl >/dev/null 2>&1; then
  mkdir -p "$DATA_DIR"
  cat > /etc/systemd/system/plorigo-agent.service <<EOF
[Unit]
Description=Plorigo server agent
After=network-online.target docker.service
Wants=network-online.target

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
  systemctl daemon-reload
  systemctl enable --now plorigo-agent
  echo "Plorigo agent installed and started. Check: systemctl status plorigo-agent"
else
  echo "Not root or systemd unavailable; starting the agent in the foreground."
  exec "$BIN_PATH" --control-plane "$CONTROL_PLANE" --token "$TOKEN" --data-dir "$DATA_DIR"
fi
