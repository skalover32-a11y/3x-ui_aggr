# Security Policy

This repository contains a Go + React server monitoring/panel aggregator with SSH operations, backup jobs, node-agent integration, WebAuthn/TOTP-related auth dependencies, Prometheus file discovery and admin workflows.

A formal independent third-party security audit has not yet been completed. The project is open to responsible disclosure.

## Supported Versions and Branches

| Branch or version | Status |
| --- | --- |
| `main` | Supported for current development and security fixes |
| Current deployment built from this repo | Supported when the commit can be identified |
| Older MVP snapshots | Best-effort only |

TODO: define release tags and support windows.

## Reporting a Vulnerability

- Security contact: TODO: add public security contact
- Report privately with affected route/module, reproduction steps and impact.
- Use lab nodes and synthetic credentials.
- Redact JWTs, refresh tokens, invite codes, SSH private keys, sudo passwords, panel credentials, WebAuthn data, backup target secrets and node-agent tokens.

Do not publish exploit details, production node data, private IPs, SSH material or backup contents before coordination.

## Scope

In scope:

- Go backend APIs, auth/session handling and admin workflows.
- SSH operations, node-agent API, file browser and database viewer launch flows.
- Backup Center secrets, schedules, storage targets and run logs.
- Prometheus file service-discovery generation.
- Docker/deployment configuration.
- React frontend behavior that affects security-sensitive workflows.

Out of scope unless caused by this code:

- social engineering;
- DDoS against monitored nodes or third-party services;
- compromise of remote panels, SSH hosts, storage providers or Telegram;
- weak production secrets after documented placeholders were not replaced.

## Expected Response Process

The maintainer process is best-effort: acknowledge, reproduce, triage, fix or mitigate, and coordinate disclosure. TODO: define response-time targets and severity levels.

## Safe Harbor

Good-faith research is welcome when it avoids service disruption, destructive SSH operations, data exfiltration and testing against systems you do not own.
