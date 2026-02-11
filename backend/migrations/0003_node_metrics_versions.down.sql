DROP TABLE IF EXISTS node_metrics;

ALTER TABLE nodes
  DROP COLUMN IF EXISTS runtime_version,
  DROP COLUMN IF EXISTS service_version,
  DROP COLUMN IF EXISTS versions_checked_at;

