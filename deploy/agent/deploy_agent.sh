#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ROOT_DIR:-/opt/vlf_aggregator}"
ENV_FILE="${ENV_FILE:-${ROOT_DIR}/.env}"

AGENT_PORT=9191
SSH_PORT=22
STATS_MODE="log"
ACTIVITY_LOG_PATH="/var/log/vlf-agent/activity.log"
RATE_LIMIT_RPS=5
ENABLE_UFW=true
TOKEN_MODE="per-node"
SHARED_TOKEN=""
ALLOW_CIDR=""
SSH_HOST=""
SSH_USER=""
SSH_KEY=""

usage() {
  cat <<'USAGE'
Usage:
  deploy_agent.sh --host <ip> --user <ssh_user> --key <path> [options]

Options:
  --port <ssh_port>          SSH port (default 22)
  --agent-port <port>        Agent port (default 9191)
  --allow-cidr <cidr>        Allow CIDR for agent (default AGG_ALLOW_CIDR from .env)
  --token-mode <per-node|shared>
  --shared-token <token>     Required if token-mode=shared
  --stats-mode <log|api>
  --activity-log <path>
  --rate-limit <rps>
  --enable-ufw | --no-ufw
  --env-file <path>          Path to aggregator .env (default /opt/vlf_aggregator/.env)
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) SSH_HOST="$2"; shift 2 ;;
    --user) SSH_USER="$2"; shift 2 ;;
    --key) SSH_KEY="$2"; shift 2 ;;
    --port) SSH_PORT="$2"; shift 2 ;;
    --agent-port) AGENT_PORT="$2"; shift 2 ;;
    --allow-cidr) ALLOW_CIDR="$2"; shift 2 ;;
    --token-mode) TOKEN_MODE="$2"; shift 2 ;;
    --shared-token) SHARED_TOKEN="$2"; shift 2 ;;
    --stats-mode) STATS_MODE="$2"; shift 2 ;;
    --activity-log) ACTIVITY_LOG_PATH="$2"; shift 2 ;;
    --rate-limit) RATE_LIMIT_RPS="$2"; shift 2 ;;
    --enable-ufw) ENABLE_UFW=true; shift ;;
    --no-ufw) ENABLE_UFW=false; shift ;;
    --env-file) ENV_FILE="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown arg: $1"; usage; exit 1 ;;
  esac
done

if [[ -z "$SSH_HOST" || -z "$SSH_USER" || -z "$SSH_KEY" ]]; then
  echo "Missing required SSH params."
  usage
  exit 1
fi

read_env_var() {
  local key="$1"
  local line
  line="$(grep -E "^${key}=" "$ENV_FILE" 2>/dev/null | tail -n1 || true)"
  line="${line#*=}"
  line="${line%\"}"
  line="${line#\"}"
  echo "$line"
}

if [[ -z "$ALLOW_CIDR" ]]; then
  ALLOW_CIDR="$(read_env_var "AGG_ALLOW_CIDR")"
fi
if [[ -z "$ALLOW_CIDR" ]]; then
  echo "ALLOW_CIDR is required (pass --allow-cidr or set AGG_ALLOW_CIDR)."
  exit 1
fi

SUDO_PASSWORDS_RAW="$(read_env_var "SUDO_PASSWORDS")"
if [[ -z "$SUDO_PASSWORDS_RAW" ]]; then
  echo "SUDO_PASSWORDS is required in ${ENV_FILE}."
  exit 1
fi
IFS=',' read -r -a SUDO_PASSWORDS <<<"$SUDO_PASSWORDS_RAW"

generate_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32 | tr -d '\n'
    return
  fi
  head -c 32 /dev/urandom | base64 | tr -d '\n'
}

TOKEN="$SHARED_TOKEN"
if [[ "$TOKEN_MODE" == "per-node" ]]; then
  TOKEN="$(generate_token)"
fi
if [[ "$TOKEN_MODE" == "shared" && -z "$TOKEN" ]]; then
  echo "Shared token required for token-mode=shared"
  exit 1
fi

echo "Building agent binary..."
pushd "${ROOT_DIR}/backend" >/dev/null
go build -o /tmp/vlf-agent ./cmd/node-agent
popd >/dev/null

tmp_config="$(mktemp)"
tmp_service="$(mktemp)"
trap 'rm -f "$tmp_config" "$tmp_service"' EXIT
cat > "$tmp_config" <<EOF
listen: "0.0.0.0:${AGENT_PORT}"
token: "${TOKEN}"
allow_cidrs:
  - "${ALLOW_CIDR}"
activity_log_path: "${ACTIVITY_LOG_PATH}"
poll_window_seconds: 60
stats_mode: "${STATS_MODE}"
rate_limit_rps: ${RATE_LIMIT_RPS}
EOF
cp "${ROOT_DIR}/deploy/agent/vlf-agent.service" "$tmp_service"

SSH_BASE=(ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$SSH_KEY" -p "$SSH_PORT" "${SSH_USER}@${SSH_HOST}")
SCP_BASE=(scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$SSH_KEY" -P "$SSH_PORT")

echo "Checking sudo..."
USE_PASS=false
SUDO_PASS=""
if "${SSH_BASE[@]}" "sudo -n true" >/dev/null 2>&1; then
  USE_PASS=false
else
  for pass in "${SUDO_PASSWORDS[@]}"; do
    pass_trim="${pass//[[:space:]]/}"
    [[ -z "$pass_trim" ]] && continue
    pass_esc=$(printf '%s' "$pass" | sed "s/'/'\"'\"'/g")
    if "${SSH_BASE[@]}" "echo '${pass_esc}' | sudo -S -p '' true" >/dev/null 2>&1; then
      USE_PASS=true
      SUDO_PASS="$pass"
      break
    fi
  done
fi
if [[ "$USE_PASS" == "true" && -z "$SUDO_PASS" ]]; then
  echo "sudo password required but not found in SUDO_PASSWORDS."
  exit 1
fi

sudo_cmd() {
  local cmd="$1"
  if [[ "$USE_PASS" == "true" ]]; then
    local pass_esc
    pass_esc=$(printf '%s' "$SUDO_PASS" | sed "s/'/'\"'\"'/g")
    echo "echo '${pass_esc}' | sudo -S -p '' $cmd"
  else
    echo "sudo -n $cmd"
  fi
}

echo "Uploading artifacts..."
"${SCP_BASE[@]}" /tmp/vlf-agent "${SSH_USER}@${SSH_HOST}:/tmp/vlf-agent"
"${SCP_BASE[@]}" "$tmp_config" "${SSH_USER}@${SSH_HOST}:/tmp/vlf-agent.yaml"
"${SCP_BASE[@]}" "$tmp_service" "${SSH_USER}@${SSH_HOST}:/tmp/vlf-agent.service"

echo "Installing agent..."
"${SSH_BASE[@]}" "$(sudo_cmd "install -m 755 /tmp/vlf-agent /usr/local/bin/vlf-agent")"
"${SSH_BASE[@]}" "$(sudo_cmd "mkdir -p /etc/vlf-agent")"
"${SSH_BASE[@]}" "$(sudo_cmd "install -m 600 /tmp/vlf-agent.yaml /etc/vlf-agent/config.yaml")"
"${SSH_BASE[@]}" "$(sudo_cmd "install -m 644 /tmp/vlf-agent.service /etc/systemd/system/vlf-agent.service")"
"${SSH_BASE[@]}" "$(sudo_cmd "systemctl daemon-reload")"
"${SSH_BASE[@]}" "$(sudo_cmd "systemctl enable --now vlf-agent")"
"${SSH_BASE[@]}" "$(sudo_cmd "systemctl restart vlf-agent")"

if [[ "$ENABLE_UFW" == "true" ]]; then
  "${SSH_BASE[@]}" "$(sudo_cmd "ufw allow from ${ALLOW_CIDR} to any port ${AGENT_PORT} proto tcp")"
fi

echo "Health check..."
"${SSH_BASE[@]}" "curl -fsS http://127.0.0.1:${AGENT_PORT}/health" >/dev/null

echo "Done."
echo "Agent URL: http://${SSH_HOST}:${AGENT_PORT}"
echo "Agent token: ${TOKEN}"

