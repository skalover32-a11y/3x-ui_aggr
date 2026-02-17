# server monitoring Panels Aggregator (MVP)

MVP aggregator for managing multiple server monitoring panels: nodes list, connections CRUD, Runtime restart, and server reboot over SSH.

## Stack
- Backend: Go 1.22 + Gin
- DB: Postgres, migrations via `golang-migrate`
- ORM: gorm
- Frontend: Vite + React

## Quick start (docker-compose)
1. Generate a master key (32 bytes, base64):
   ```bash
   python - <<'PY'
   import os, base64
   print(base64.b64encode(os.urandom(32)).decode())
   PY
   ```
2. Create `.env` next to `docker-compose.yml`:
   ```env
   AGG_MASTER_KEY_BASE64=...
   ADMIN_USER=admin
   ADMIN_PASS=admin123
   JWT_SECRET=supersecretjwt
   TOKEN_SALT=change-me
   ```
3. Run (postgres -> migrate -> backend -> frontend):
   ```bash
   docker compose up -d --build
   ```
4. Open the frontend: http://localhost:5173

## Local run
```bash
make migrate-up
make run
```

## Backend env vars
- `DB_DSN` (required) example: `postgres://agg:agg@localhost:5432/agg?sslmode=disable`
- `AGG_MASTER_KEY_BASE64` (required) 32 bytes, base64
- `ADMIN_USER` / `ADMIN_PASS` (required)
- `JWT_SECRET` (required)
- `TOKEN_SALT` (required, used to hash agent/registration tokens)
- `ACCESS_TOKEN_TTL` (optional, default `24h`, overrides `JWT_EXP_HOURS`)
- `JWT_EXP_HOURS` (optional, default 24, legacy)
- `REFRESH_TOKEN_TTL` (optional, default `720h`)
- `AUTH_RP_ID` (optional, WebAuthn RP ID, example: `aggr.example.com`)
- `AUTH_RP_ORIGIN` (optional, WebAuthn RP origin, example: `https://aggr.example.com`)
- `FILE_ALLOWED_ROOTS` (optional, default `/opt,/var/log,/home/*/backups`)
- `FILE_PREVIEW_MAX_BYTES` (optional, default `2097152`)
- `FILE_TAIL_MAX_BYTES` (optional, default `131072`)
- `PORT` (optional, default 8080)
- `GLOBAL_MAX_SSH_SESSIONS` (optional, default 10)
- `SSH_IDLE_TIMEOUT_SECONDS` (optional, default 600)
- `PUBLIC_BASE_URL` (optional, for Telegram alert buttons; required to build agent install_command, example: `https://aggr.example.com`)
- `TELEGRAM_WEBHOOK_SECRET` (optional legacy fallback secret for Telegram webhook; normally webhook secret is configured automatically from UI)
- `ALERT_CPU_THRESHOLD` (optional, default `2.0`, CPU/load threshold for CPU alerts)
- `ALERT_MEMORY_THRESHOLD` (optional, default `90.0`, percent used RAM threshold)
- `ALERT_DISK_FREE_THRESHOLD` (optional, default `10.0`, percent free disk threshold)
- `ALERT_OFFLINE_DELAY` (optional, default `5m`, delay before sending offline fail alerts)
- `ALERT_MIN_CONSECUTIVE_FAILS` (optional, default `2`, minimum consecutive `fail` samples before sending new `connection`/`generic`/`tls` alert to Telegram)
- `NODE_AGENT_ADDR` (node-agent, optional, default `:9090`)
- `NODE_AGENT_TOKEN` (node-agent, optional bearer token)
- `NODE_AGENT_ALLOWLIST` (node-agent, optional comma-separated IP allowlist)
- `DASHBOARD_COLLECT_INTERVAL` (optional, default `10s`)
- `DASHBOARD_COLLECT_PARALLELISM` (optional, default `5`)
- `DASHBOARD_COLLECT_TIMEOUT` (optional, default `8s`)
- `DASHBOARD_PANEL_ACTIVE_USERS_ENABLED` (optional, default `true`)
- `DASHBOARD_PANEL_SESSION_TTL` (optional, default `12h`, set `0` to keep session until panel expires it)
- `DASHBOARD_AGENT_TIMEOUT` (optional, default `5s`)
- `DASHBOARD_AGENT_PREFER` (optional, default `true`)
- `SUDO_PASSWORDS` (optional, comma-separated sudo passwords for ops jobs like deploy agent)
- `AGG_ALLOW_CIDR` (optional, default allow CIDR for agent deploy)
- `AGG_REPO_PATH` (optional, default `/opt/vlf_aggregator`, used to build vlf-agent)

## Invite-only signup
Registration is invite-only. Admin creates invites, users sign up with invite code.

Create invite (admin JWT required):
```bash
curl -s http://localhost:8080/api/admin/invites \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"expires_in_hours":168,"org_name":"Personal","role":"owner"}'
```

Signup with invite:
```bash
curl -s http://localhost:8080/api/signup \
  -H "Content-Type: application/json" \
  -d '{"invite_code":"INV_xxx","username":"user","password":"strongpass123"}'
```

## Node types
- **PANEL**: server monitoring panel node. Requires `base_url`, `panel_username`, `panel_password`.
- **HOST**: SSH-only node. No panel required; use service checks for HTTP endpoints.
- **BOT (UI)**: creates a HOST node with tag `bot` and opens the bot creation form.

Example HOST node create:
```bash
curl -s http://localhost:8080/api/nodes \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "kind": "HOST",
    "name": "host-1",
    "tags": ["infra"],
    "ssh_host": "1.2.3.4",
    "ssh_port": 22,
    "ssh_user": "root",
    "ssh_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----"
  }'
```

## Files (SFTP)
The **Files** section provides a mini file browser over SFTP for a selected node. Access is limited to allowed roots:
- Global default allowlist via `FILE_ALLOWED_ROOTS`
- Optional per-node override via `allowed_roots` (array of paths)

Examples:
```bash
curl -s http://localhost:8080/api/nodes/<node_id>/files/roots \
  -H "Authorization: Bearer <token>"

curl -s "http://localhost:8080/api/nodes/<node_id>/files/list?path=/var/log" \
  -H "Authorization: Bearer <token>"

curl -s "http://localhost:8080/api/nodes/<node_id>/files/download?path=/var/log/syslog" \
  -H "Authorization: Bearer <token>" -o syslog
```

Update node allowed roots:
```bash
curl -s http://localhost:8080/api/nodes/<node_id> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -X PATCH \
  -d '{"allowed_roots":["/opt","/var/log","/home/*/backups"]}'
```

## DB work (node-agent)
The **DB work** section opens database viewers through the node-agent proxy (no public DB ports).
- SQLite uses sqlite-web (read-only)
- Postgres/MySQL uses Adminer
SQLite can be toggled to read-write from the UI (off by default).

Agent requirements:
- Docker installed on the node
- Allowlist configured for the aggregator IP
- Optional config in `/etc/vlf-agent/config.yaml`:
  - `adminer_port` (default 18081)
  - `sqlite_port` (default 18082)
  - `sqlite_roots` (default `/opt`, `/var/lib`)

API endpoints (admin only):
- `GET /api/nodes/<node_id>/db/sqlite/list`
- `POST /api/nodes/<node_id>/db/sqlite/start`
- `POST /api/nodes/<node_id>/db/adminer/start`

## Ops jobs (bulk operations)
Create a reboot job for all nodes:
```bash
curl -s http://localhost:8080/api/ops/jobs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"reboot_nodes","all":true,"parallelism":5,"params":{"confirm":"REALLY_DO_IT"}}'
```

Update service-manager on selected nodes (SSH + expect):
```bash
curl -s http://localhost:8080/api/ops/jobs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"update_nodes","node_ids":["<node_id_1>","<node_id_2>"],"parallelism":3,"params":{"precheck_only":false,"install_expect":false}}'
```

Update notes:
- Uses `expect` with menu prompt: `Please enter your selection [0-25]:`
- Uses `flock -n /var/lock/service-manager-update.lock` to prevent concurrent updates
- Timeouts: menu 60s, update 900s
- `precheck_only=true` runs diagnostics only (no update)
- `sandbox=true` restricts targets to nodes with `is_sandbox=true`

Deploy agent on selected nodes:
```bash
curl -s http://localhost:8080/api/ops/deploy-agent \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "node_ids":["<node_id_1>","<node_id_2>"],
    "parallelism":2,
    "params":{
      "agent_port":9191,
      "agent_token_mode":"per-node",
      "allow_cidr":"<AGG_IP>/32",
      "stats_mode":"log",
      "activity_log_path":"/var/log/vlf-agent/activity.log",
      "rate_limit_rps":5,
      "enable_ufw":true,
      "health_check":true
    }
  }'
```

Notes:
- `deploy_agent` uses a prebuilt agent binary baked into the backend image at `/app/bin/vlf-agent`.
- After pulling changes, rebuild backend: `docker compose up -d --build backend`
- Verify inside container:
  `docker compose exec backend sh -lc "ls -la /app/bin/vlf-agent"`

## Agent tasks (control plane, no SSH)
Agent is the single control plane for bulk actions (update/reboot/restart). Backend never SSH-es for these tasks.
Nodes must have `agent_enabled=true` + `agent_url` + `agent_token`.

Bulk update panels:
```bash
curl -s http://localhost:8080/api/tasks/bulk \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"update_services","node_ids":["<node_id_1>","<node_id_2>"],"parallelism":3,"params":{}}'
```

Bulk reboot nodes:
```bash
curl -s http://localhost:8080/api/tasks/bulk \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"reboot_node","node_ids":["<node_id_1>"],"parallelism":2,"params":{}}'
```
Note: the agent API expects confirm="REBOOT", the backend sends it automatically.

Bulk restart service (whitelist):
```bash
curl -s http://localhost:8080/api/tasks/bulk \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"restart_service","node_ids":["<node_id_1>"],"parallelism":2,"params":{"restart_service":"Runtime"}}'
```

Get job and items:
```bash
curl -s http://localhost:8080/api/ops/jobs/<job_id> \
  -H "Authorization: Bearer <token>"

curl -s http://localhost:8080/api/ops/jobs/<job_id>/items \
  -H "Authorization: Bearer <token>"
```

Debug ops access (no OTP) using master key:
```bash
curl -s http://localhost:8080/api/ops/jobs/<job_id> \
  -H "X-Agg-Master-Key: <AGG_MASTER_KEY_BASE64>"
```
Notes:
- Master auth only works when `AGG_MASTER_KEY_BASE64` is set.
- If `AGG_ALLOW_CIDR` is set, the client IP must be within that CIDR (localhost always allowed).

## TLS status reasons (panel checks)
Panel TLS failures are classified and exposed in the node status/uptime API:
`CERT_EXPIRED`, `CERT_NOT_YET_VALID`, `UNKNOWN_CA`, `HOSTNAME_MISMATCH`, `HANDSHAKE`, `GENERIC_HTTP_ERROR`.
UI shows a short label (for example: "TLS certificate expired") with full detail in tooltip.

## Telegram inline buttons
Telegram callback_data is limited to <= 64 bytes. We use short IDs:
`ack:<alert_id>`, `mute:<alert_id>:<minutes>`, `retry:<alert_id>`, `open:<alert_id>`.
Long data (fingerprints, URLs, TLS errors) stays in the DB.

Public job status (no login, per-job token):
```bash
curl -s "http://localhost:8080/api/ops/jobs/<job_id>/public?token=<public_token>"
```
Notes:
- `public_token` is returned on job creation (`POST /api/ops/jobs`, `POST /api/ops/deploy-agent`).

## Dashboard (realtime)
Dashboard data is collected via node-agent (`/stats`, `/active-users`). SSH is not used for dashboard telemetry.
If agent active users are not available, panel API can be used when `DASHBOARD_PANEL_ACTIVE_USERS_ENABLED=true`.
Panel version is reported by the agent and shown in the dashboard table. Average ping comes from agent `ping_ms`.
Total traffic (24h/7d) is calculated from time-series `node_metrics` net byte counters.

Summary (nodes + aggregates):
```bash
curl -s http://localhost:8080/api/dashboard/summary \
  -H "Authorization: Bearer <token>"
```

Active users (latest):
```bash
curl -s "http://localhost:8080/api/dashboard/active-users?limit=200&search=" \
  -H "Authorization: Bearer <token>"
```

WebSocket stream (use token query):
```
ws://localhost:8080/api/dashboard/stream?token=<token>
```
Authorization header is preferred when available:
```
Authorization: Bearer <token>
```

## Node agent (v1)
You can deploy the agent either via the UI (**Deploy agent**) or via CLI (see `deploy/agent/deploy_agent.sh`).

1) Build agent binary:
```bash
cd backend
go build -o vlf-agent ./cmd/node-agent
```
2) Copy deploy artifacts:
```bash
sudo mkdir -p /etc/vlf-agent
sudo cp deploy/agent/config.example.yaml /etc/vlf-agent/config.yaml
sudo cp deploy/agent/vlf-agent.service /etc/systemd/system/vlf-agent.service
sudo cp backend/vlf-agent /usr/local/bin/vlf-agent
```
3) Edit `/etc/vlf-agent/config.yaml`:
```yaml
listen: "0.0.0.0:9191"
token: "CHANGE_ME"
# Optional: multiple tokens separated by commas
# token: "tokenA,tokenB"
allow_cidrs:
  - "<AGG_IP>/32"
activity_log_path: "/var/log/vlf-agent/activity.log"
poll_window_seconds: 60
stats_mode: "log"
rate_limit_rps: 5
```
4) Start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now vlf-agent
```
5) Open port 9191 only for aggregator IP.
6) Enable agent in node:
```bash
curl -s http://localhost:8080/api/nodes/<node_id> \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -X PATCH \
  -d '{"agent_enabled":true,"agent_url":"http://<node-ip>:9191","agent_token":"CHANGE_ME"}'
```

CLI deploy (uses `/opt/vlf_aggregator/.env` for `SUDO_PASSWORDS` + `AGG_ALLOW_CIDR`):
```bash
deploy/agent/deploy_agent.sh --host <node-ip> --user <ssh_user> --key /path/to/key
```

### Safe testing in prod (dry-run)
Dry-run (no SSH, simulated execution):
```bash
curl -s http://localhost:8080/api/ops/jobs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"update_nodes","all":true,"parallelism":5,"params":{"dry_run":true,"simulate_delay_ms":500}}'
```

Precheck only (no update, diagnostics only):
```bash
curl -s http://localhost:8080/api/ops/jobs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"update_nodes","node_ids":["<node_id>"],"parallelism":1,"params":{"precheck_only":true}}'
```

Real run with confirmation (all=true):
```bash
curl -s http://localhost:8080/api/ops/jobs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"update_nodes","all":true,"parallelism":5,"params":{"confirm":"REALLY_DO_IT"}}'
```

## DB reset
```bash
docker compose down -v
```

## Migration status (optional)
```bash
docker compose run --rm migrate -path /migrations -database "postgres://agg:agg@postgres:5432/agg?sslmode=disable" version
```

## API examples
Login:
```bash
curl -s http://localhost:8080/api/auth/login \
  -H "X-Requested-With: XMLHttpRequest" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123","otp":"123456"}'
```

Login with 2FA:
```bash
curl -s http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"operator1","password":"pass","otp":"123456"}'
```

Refresh access token (uses `agg_refresh` cookie):
```bash
curl -s http://localhost:8080/api/auth/refresh \
  -H "Content-Type: application/json" \
  -H "X-Requested-With: XMLHttpRequest" \
  --cookie "agg_refresh=<refresh_token>"
```

Logout (revokes refresh cookie):
```bash
curl -s http://localhost:8080/api/auth/logout \
  -H "Content-Type: application/json" \
  -H "X-Requested-With: XMLHttpRequest" \
  --cookie "agg_refresh=<refresh_token>"
```

Request a recovery code (sent to Telegram admin chats):
```bash
curl -s http://localhost:8080/api/auth/2fa/recovery \
  -H "Content-Type: application/json" \
  -d '{"username":"operator1","password":"pass"}'
```

2FA setup flow (authenticated):
```bash
curl -s http://localhost:8080/api/auth/2fa/status \
  -H "Authorization: Bearer <token>"

curl -s http://localhost:8080/api/auth/2fa/setup \
  -H "Authorization: Bearer <token>"

curl -s http://localhost:8080/api/auth/2fa/verify \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"code":"123456"}'
```

## Passkeys (WebAuthn)
1. Login with password (and 2FA if enabled).
2. Open menu -> **Passkeys** -> **Enable Passkey** (enter OTP if 2FA is enabled).
3. Logout. Next time use **Login with Passkey** on the login screen.

The API issues a short-lived access token and a long-lived refresh cookie (`agg_refresh`) to remember the device.
Canonical WebAuthn tables are `webauthn_credentials` and `webauthn_challenges`. Legacy `web_authn_*` names are compatibility-only views.

Passkeys (authenticated):
```bash
curl -s http://localhost:8080/api/auth/webauthn/register/options \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"otp":"123456"}'
```

Passkeys login (passkey ceremony):
```bash
curl -s http://localhost:8080/api/auth/webauthn/login/options \
  -H "Content-Type: application/json" \
  -d '{"username":"admin"}'
```

List connections:
```bash
curl -s http://localhost:8080/api/nodes/<node_id>/connections \
  -H "Authorization: Bearer <token>"
```

Node status:
```bash
curl -s http://localhost:8080/api/nodes/<node_id>/status \
  -H "Authorization: Bearer <token>"
```


Health check:
```bash
curl -s http://localhost:8080/api/healthz
```

Agent ping (backend):
```bash
curl -s http://localhost:8080/api/agent/ping
curl -s "http://localhost:8080/api/agent/ping?node_id=<node_id>"
```

List services:
```bash
curl -s http://localhost:8080/api/services \
  -H "Authorization: Bearer <token>"
```

## Manual test checklist
- Login with password (and OTP if enabled) works.
- Passkey registration succeeds from the Passkeys menu.
- Logout clears session and refresh cookie.
- Login with Passkey works.
- Reload app triggers `/api/auth/refresh` and restores session.

Create service (global):
```bash
curl -s http://localhost:8080/api/services \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"<node_id>","kind":"CUSTOM_HTTP","url":"https://example.com","health_path":"/","expected_status":[200]}'
```

Run service check now:
```bash
curl -s -X POST http://localhost:8080/api/services/<service_id>/run \
  -H "Authorization: Bearer <token>"
```

Service results (last 60 minutes):
```bash
curl -s "http://localhost:8080/api/services/<service_id>/results?minutes=60" \
  -H "Authorization: Bearer <token>"
```

Cleanup test services by URL pattern:
```bash
make cleanup-services PATTERN='%example.com%'
```

Node uptime (last 60 minutes):
```bash
curl -s "http://localhost:8080/api/nodes/<node_id>/uptime?minutes=60" \
  -H "Authorization: Bearer <token>"
```

Restart Runtime:
```bash
curl -s -X POST http://localhost:8080/api/nodes/<node_id>/actions/restart-Runtime \
  -H "Authorization: Bearer <token>"
```

Reboot:
```bash
curl -s -X POST http://localhost:8080/api/nodes/<node_id>/actions/reboot \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"confirm":"REBOOT"}'
```

Create node:
```bash
curl -s http://localhost:8080/api/nodes \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "node-1",
    "tags": ["ru","prod"],
    "base_url": "https://example.com",
    "panel_username": "admin",
    "panel_password": "pass",
    "ssh_host": "1.2.3.4",
    "ssh_port": 22,
    "ssh_user": "root",
    "ssh_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----",
    "verify_tls": true
  }'
```

## Notes
- `settings` and `streamSettings` can be sent as JSON objects; they are serialized to strings when needed for server monitoring.
- Update source entry is a merge patch: unspecified fields are preserved.
- Audit logs are written for add/update/delete source entry, restart Runtime, and reboot.
- SSH key upload: use the UI "Upload SSH Key (.ppk/.pem/.key)" and optionally enter a passphrase for encrypted PPKs; the backend converts to an OpenSSH-compatible private key and shows a fingerprint.
- Web SSH: use the "SSH" button on a node card (admin only). The browser terminal connects via WebSocket; SSH keys never leave the server.
- Web SSH limits: global max sessions (`GLOBAL_MAX_SSH_SESSIONS`) and idle timeout (`SSH_IDLE_TIMEOUT_SECONDS`).

## Services & checks (UI)
1. Open a node and switch to the **Services** tab.
2. Click **Add** and fill:
   - `URL` (base or full URL)
   - `Health path` (e.g. `/` or `/health`)
   - `Expected status` list (defaults to `200`)
3. Backend auto-creates a default HTTP check for each service:
   - interval: 60 sec
   - timeout: 3000 ms
   - retries: 1
4. Use **Run now** to trigger an immediate check; results update in the table.
5. Use **Disable/Enable** to stop/resume checks for a service.
6. Runner starts automatically with the backend (no extra ENV required). On startup it backfills checks for services without one.

## Bots monitoring
Bots are monitored via the same checks/results/alerts pipeline.

Kinds:
- **HTTP**: checks `health_url` + `health_path`, expected status list (default `200`).
- **DOCKER**: checks container running state via SSH.
- **SYSTEMD**: checks `systemctl is-active` via SSH.

UI flow:
1. Switch to **Bots** in the top filter, or open a node and the **Bots** tab.
2. Click **Add** and choose bot kind + target.
3. A default check is auto-created (interval 30s, timeout 3000ms, retries 1).
4. Use **Run now** for an immediate check; results appear in the table.
5. **Mute 1h** suppresses duplicate Telegram alerts for that bot.

API examples:
```bash
# Create bot for a node
curl -s http://localhost:8080/api/nodes/<node_id>/bots \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"bot-1","kind":"HTTP","health_url":"https://example.com","health_path":"/","expected_status":[200]}'

# Run bot check now
curl -s -X POST http://localhost:8080/api/bots/<bot_id>/run-now \
  -H "Authorization: Bearer <token>"

# Bot results (last 60 minutes)
curl -s "http://localhost:8080/api/bots/<bot_id>/results?minutes=60" \
  -H "Authorization: Bearer <token>"
```

## Alerts: mute / retry / run-now
- **Mute** sets `muted_until` and suppresses repeated notifications for that alert fingerprint.
- Mute/Ack TTL are org-scoped and configurable in Telegram settings UI (`mute_minutes`, `ack_mute_minutes`).
- **Retry** triggers an immediate run-now check for the related service/bot.
- When a failed check becomes **ok**, a recovery message is sent/edited.
- Telegram notifications are deduplicated per fingerprint (same status) for 5 minutes.

## Telegram alerts
Alerts are sent in Telegram HTML format with inline buttons (open node, metrics, retry, mute).
Webhook is configured automatically when Telegram settings are saved in the UI.
Required env for links/buttons:
- `PUBLIC_BASE_URL` (example: `https://aggr.example.com`)
- `TOKEN_SALT` (used to derive per-org webhook secret automatically)

Example alert:
```
🔥 High CPU — NODE-1
load1: 2.16 (threshold 2.00) | 2025-01-02 03:04:05
Host: 1.2.3.4
Severity: WARNING
```

## Incidents, diagnostics, backup
New API endpoints:

- `GET /api/incidents?active=true&limit=200`
  - Returns org-scoped incidents.
- `POST /api/incidents/:id/ack`
  - Marks incident as acknowledged (`status=acked`), mutes linked alert state for org-configured `ack_mute_minutes`.
- `GET /api/nodes/:id/diagnostics`
  - Returns extended node diagnostics: latest metrics, latest node check, service/bot counters, incident summary.
- `GET /api/orgs/:orgId/export`
  - Exports org configuration backup (nodes, services, bots, checks, keys) as JSON.
- `POST /api/orgs/:orgId/import`
  - Imports org configuration backup into current org (replace mode for org-scoped data).
- `POST /api/orgs/:orgId/import?dry_run=1`
  - Validates backup payload and returns preview (`incoming/existing/valid/skipped` + warnings) without applying changes.


