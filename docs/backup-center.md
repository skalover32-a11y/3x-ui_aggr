# Backup Center

## What it is

`Backup Center` is the org-scoped backup subsystem inside the aggregator. It stores backup jobs, sources, storage targets, run history and built-in templates in Postgres and executes runs through the backend runner.

The current execution model is:

- backup job config lives in DB
- storage secrets are encrypted at rest
- scheduler enqueues due jobs from cron
- backend creates a per-run workdir under `AGG_DATA_DIR`
- sources are collected one by one into separate artifacts
- `SHA256SUMS` manifest is generated
- artifacts are uploaded through a storage adapter
- run/job status and per-item status are persisted

The design is ready for a future move of the execution layer into node agents. Today it can already bind a job to a selected node and use the existing SSH/node abstraction for remote-aware handlers and catalogs.

## Storage targets

Supported target types:

- FTP
- FTPS
- SFTP
- WebDAV
- S3-compatible
- Local path

Secrets are never returned to the UI after save. The API only returns masked config fields such as:

- `password_set`
- `private_key_set`
- `secret_key_set`

## Source types

Supported source types:

- `file_path`
- `directory_path`
- `docker_volume`
- `postgres_container_dump`
- `postgres_manual_dump`
- `nginx_snapshot`
- `cron_snapshot`
- `docker_inventory_snapshot`
- `system_snapshot`

Prepared but intentionally blocked in v1:

- `custom_command` requires explicit unsafe enablement in backend
- MySQL source types are reserved in the domain model but not implemented yet

## Built-in templates

The backend seeds these templates on startup:

- Website Server
- Remnawave
- Aggregator Node
- Vaultwarden
- Docker app + PostgreSQL
- Generic Linux server

Templates are stored in `backup_templates` and are copied into editable job sources when applied.

## Required env

Backup Center uses the existing backend env:

- `AGG_MASTER_KEY_BASE64`: required; used for encrypted-at-rest storage target secrets
- `AGG_DATA_DIR`: optional, defaults to `./data`
- `SUDO_PASSWORDS`: optional; used by handlers that need privileged access

Runtime data is stored under:

- `AGG_DATA_DIR/backup/runs/<run-id>/`

Typical per-run contents:

- collected archives and dumps
- snapshot files
- `SHA256SUMS`
- `run.log`

## Manual usage

1. Open `Backup Center`
2. Create a storage target and run `Test`
3. Create a job or apply a template
4. Add or edit sources
5. Save the job
6. Run `Run now`
7. Open `Runs` to inspect per-item results and logs

## Retention

Current v1 retention:

- `retention_days` on the job

The runner cleans old remote artifacts through the storage adapter after upload. The data model is ready for future `keep_count` and multi-schedule policies.

## Security model

- backup APIs are org-scoped
- read endpoints require org `viewer`
- mutating endpoints require org `admin`
- storage secrets are encrypted with the backend encryptor
- secrets are masked in responses
- path inputs are validated and absolute
- path traversal via `..` segments is rejected
- arbitrary custom commands are blocked unless explicitly enabled server-side

## Metrics

The backup subsystem exports Prometheus metrics for:

- total runs by status
- run duration
- uploaded bytes
- last success timestamp
- enabled jobs count

## Extending sources

Add a new source handler in the backup runner layer:

1. define config struct and source type in `backend/internal/services/backup/types.go`
2. validate it in `backend/internal/services/backup/validation.go`
3. wire collection logic in `backend/internal/services/backup/runner.go`
4. expose it in frontend source options
5. add tests

## Extending storage

Add a new storage adapter by:

1. implementing `backend/internal/services/backup/storage/types.go`
2. wiring the target in `backend/internal/services/backup/storage/factory.go`
3. validating target config in `backend/internal/services/backup/validation.go`
4. exposing config fields in `BackupTargetModal.jsx`

## Troubleshooting

If a run fails, inspect:

- job details in `Backup Center -> Runs`
- per-item status in the run modal
- stored run log from `GET /backup/runs/:id/log`
- backend logs if the runner failed before persisting item-level context

Typical failure stages:

- source validation
- archive/dump collection
- upload
- retention cleanup
