#!/usr/bin/env bash
set -euo pipefail

BIN_SRC="${1:-./vlf-agent}"
BIN_DST="/usr/local/bin/vlf-agent"
CONF_DST="/etc/vlf-agent/config.yaml"
SERVICE_DST="/etc/systemd/system/vlf-agent.service"

mkdir -p /etc/vlf-agent

if [ ! -f "$BIN_SRC" ]; then
  echo "Binary not found: $BIN_SRC"
  exit 1
fi

install -m 755 "$BIN_SRC" "$BIN_DST"
if [ ! -f "$CONF_DST" ]; then
  install -m 600 "$(dirname "$0")/config.example.yaml" "$CONF_DST"
fi
install -m 644 "$(dirname "$0")/vlf-agent.service" "$SERVICE_DST"

systemctl daemon-reload
systemctl enable --now vlf-agent

echo "Agent installed and started."
