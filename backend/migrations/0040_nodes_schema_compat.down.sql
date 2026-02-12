DROP INDEX IF EXISTS idx_telegram_settings_org_id;

ALTER TABLE telegram_settings
  DROP COLUMN IF EXISTS org_id;

ALTER TABLE node_metrics_latest
  DROP COLUMN IF EXISTS panel_running,
  DROP COLUMN IF EXISTS panel_version,
  DROP COLUMN IF EXISTS service_running,
  DROP COLUMN IF EXISTS service_version,
  DROP COLUMN IF EXISTS runtime_running;

ALTER TABLE nodes
  DROP COLUMN IF EXISTS versions_checked_at,
  DROP COLUMN IF EXISTS panel_version,
  DROP COLUMN IF EXISTS service_version,
  DROP COLUMN IF EXISTS runtime_version;
