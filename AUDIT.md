# Audit Status

A formal independent third-party security audit has not yet been completed.

This document tracks internal audit readiness for the server monitoring/panel aggregator.

## Current Status

| Area | Status |
| --- | --- |
| Formal third-party audit | Not completed |
| Internal self-assessment | In progress |
| Public review | Open |
| Route authorization matrix | TODO |
| Backup secret review | TODO |

## Review Checklist

- [ ] Authentication, JWT and refresh-token lifecycle.
- [ ] WebAuthn/TOTP enrollment and recovery behavior.
- [ ] Invite-only signup boundaries.
- [ ] SSH command execution and sudo password handling.
- [ ] File browser root restrictions and path traversal.
- [ ] Backup target encryption and run-log redaction.
- [ ] Node-agent token and allowlist behavior.
- [ ] Prometheus file generation and reload permissions.
- [ ] Dependency review for Go, Node and Docker images.
- [ ] Production secret rotation and incident response.

## Future External Audit Plan

TODO: define target commit, deployment topology, test node inventory and auditor access model.
