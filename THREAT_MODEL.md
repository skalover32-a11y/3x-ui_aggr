# Threat Model

A formal independent third-party security audit has not yet been completed.

## Assets

- Admin/user accounts, JWTs, refresh tokens and invite codes.
- SSH keys, sudo passwords and node credentials.
- Panel credentials and node-agent bearer tokens.
- Backup storage secrets and backup contents.
- File/database viewer access to remote nodes.
- Prometheus target data and alert configuration.
- Build and deployment configuration.

## Threat Actors

- Anonymous internet attackers.
- Authenticated users trying to access other orgs/nodes.
- Malicious or compromised admins.
- Attackers with leaked JWTs, node-agent tokens or SSH credentials.
- Compromised remote nodes or storage providers.
- Compromised dependencies or build hosts.

## Threats

- Credential leakage.
- Authorization bypass across orgs, nodes or backup jobs.
- Destructive SSH/sudo job execution.
- Path traversal or excessive access through file browser.
- Backup exfiltration or restoration to unsafe targets.
- Node-agent abuse.
- Dependency/build compromise.
- Logging of secrets in run logs or alerts.

## Mitigations Observed

- Required master key for secret encryption.
- Invite-only signup described in README.
- File allowed roots configuration.
- Node-agent token and allowlist settings.
- Docker Compose separates backend, frontend, database and Prometheus services.

## Recommended Improvements

- Add a route-by-route permission matrix.
- Add tests for path traversal and org boundary enforcement.
- Add secret redaction for backup/job logs.
- Document credential rotation and emergency node disable flows.
- Use external rate limiting for public deployments.

## Non-Goals

- Protecting remote hosts already compromised by an attacker.
- Absorbing volumetric DDoS.
- Defining service-level retention outside deployment policy.
