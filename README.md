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
