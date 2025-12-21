DROP TABLE IF EXISTS node_metrics;

ALTER TABLE nodes
  DROP COLUMN IF EXISTS xray_version,
  DROP COLUMN IF EXISTS panel_version,
  DROP COLUMN IF EXISTS versions_checked_at;
