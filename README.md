# 3x-ui Panels Aggregator (MVP)

MVP aggregator for managing multiple 3x-ui panels: nodes list, inbounds CRUD, Xray restart, and server reboot over SSH.

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
- `PUBLIC_BASE_URL` (optional, for Telegram alert buttons, example: `https://aggr.example.com`)
- `NODE_AGENT_ADDR` (node-agent, optional, default `:9090`)
- `NODE_AGENT_TOKEN` (node-agent, optional bearer token)
- `NODE_AGENT_ALLOWLIST` (node-agent, optional comma-separated IP allowlist)

## Node types
- **PANEL**: 3x-ui panel node. Requires `base_url`, `panel_username`, `panel_password`.
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

## Ops jobs (bulk operations)
Create a reboot job for all nodes:
```bash
curl -s http://localhost:8080/api/ops/jobs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"reboot_nodes","all":true,"parallelism":5,"params":{}}'
```

Get job and items:
```bash
curl -s http://localhost:8080/api/ops/jobs/<job_id> \
  -H "Authorization: Bearer <token>"

curl -s http://localhost:8080/api/ops/jobs/<job_id>/items \
  -H "Authorization: Bearer <token>"
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

List inbounds:
```bash
curl -s http://localhost:8080/api/nodes/<node_id>/inbounds \
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

Restart Xray:
```bash
curl -s -X POST http://localhost:8080/api/nodes/<node_id>/actions/restart-xray \
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
- `settings` and `streamSettings` can be sent as JSON objects; they are serialized to strings when needed for 3x-ui.
- Update inbound is a merge patch: unspecified fields are preserved.
- Audit logs are written for add/update/delete inbound, restart xray, and reboot.
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
- **Mute 1h** sets `muted_until` and suppresses repeated notifications for that alert fingerprint.
- **Retry** triggers an immediate run-now check for the related service/bot.
- When a failed check becomes **ok**, a recovery message is sent/edited.
- Telegram notifications are deduplicated per fingerprint (same status) for 5 minutes.

## Telegram alerts
Alerts are sent in Telegram HTML format with inline buttons (open node, metrics, retry, mute).
To enable callback buttons, set the webhook to:
```bash
curl -s "https://api.telegram.org/bot<token>/setWebhook?url=https://<PUBLIC_BASE_URL>/api/telegram/webhook"
```
Required env for links/buttons:
- `PUBLIC_BASE_URL` (example: `https://aggr.example.com`)

Example alert:
```
🔥 High CPU — NODE-1
load1: 2.16 (threshold 2.00) | 2025-01-02 03:04:05
Host: 1.2.3.4
Severity: WARNING
```
