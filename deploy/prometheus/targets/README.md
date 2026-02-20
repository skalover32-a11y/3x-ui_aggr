# Legacy static targets

This folder is kept for legacy/manual examples only.

Current org-scoped integration writes file_sd files automatically to:

- `./data/prom_sd/org_<org-id>.json` on host
- `/etc/prometheus/sd/org_<org-id>.json` inside Prometheus container

Prometheus config reads from `/etc/prometheus/sd/org_*.json`.

For `vlf-agent`, Prometheus scrapes `http://<target>/metrics`.
Recommended node-agent config:

- `allow_cidrs` should include your Prometheus server IP/CIDR
- `metrics_require_auth: false` (default)
