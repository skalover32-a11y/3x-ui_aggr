# Privacy Overview

This repository implements an infrastructure/panel aggregator. It is an operations tool, not an end-user VPN client.

A formal independent third-party security audit has not yet been completed.

## Data This Component May Process

- Admin/user accounts, roles, invites, JWT/refresh-token metadata and WebAuthn/TOTP-related state.
- Node inventory, tags, panel URLs, SSH hostnames/IPs, SSH usernames and service checks.
- Panel credentials and node-agent tokens.
- Sudo passwords supplied for operational jobs.
- Backup storage target configuration and encrypted storage credentials.
- File browser paths and file contents explicitly requested through SFTP.
- Database viewer targets launched through node-agent.
- Prometheus target files, health data, alert state and Telegram alert configuration.
- Backup run logs and operational job logs.

## Data It Should Not Expose

SSH keys, sudo passwords, backup target secrets, panel credentials, JWTs, refresh tokens, invite codes, node-agent tokens and private file/database contents must not be published in issues, logs or screenshots.

No traffic resale, traffic injection or ad injection is intended by project policy. This repository operates infrastructure management features and does not implement VPN traffic inspection.

## Logs and Diagnostics

Runtime logs and backup/job logs may include node names, paths, command output, failure messages and remote host metadata. Treat them as operationally sensitive.

## Third-Party Dependencies

Review Go dependencies in `backend/go.mod`, frontend dependencies in `frontend/package.json`, Docker images, Prometheus configuration and any SSH/storage libraries used by Backup Center.

## Data Retention

This repository does not define server-side retention policy. See service-level privacy policy.

Retention for database rows, backup run logs, Prometheus storage and generated files under `data/` is deployment-specific.
