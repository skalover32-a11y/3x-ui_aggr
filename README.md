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
- `JWT_EXP_HOURS` (optional, default 24)
- `PORT` (optional, default 8080)
- `GLOBAL_MAX_SSH_SESSIONS` (optional, default 10)
- `SSH_IDLE_TIMEOUT_SECONDS` (optional, default 600)
- `PUBLIC_BASE_URL` (optional, for Telegram alert buttons, example: `https://aggr.example.com`)

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
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'
```

Login with 2FA:
```bash
curl -s http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"operator1","password":"pass","otp":"123456"}'
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

## Alerts: mute / retry / run-now
- **Mute 1h** sets `muted_until` and suppresses repeated notifications for that alert fingerprint.
- **Retry** triggers an immediate run-now check for the related service.
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
