DROP INDEX IF EXISTS idx_node_metrics_traffic_cov;
DROP INDEX IF EXISTS idx_node_metrics_ts_node;

DROP INDEX IF EXISTS idx_alert_states_status_muted;
DROP INDEX IF EXISTS idx_incidents_node_id;
DROP INDEX IF EXISTS idx_incidents_org_status;
DROP INDEX IF EXISTS idx_incidents_status_last_seen;

ALTER TABLE alert_states DROP CONSTRAINT IF EXISTS alert_states_incident_id_fkey;
ALTER TABLE alert_states
  DROP COLUMN IF EXISTS incident_id,
  DROP COLUMN IF EXISTS ok_streak;

DROP TABLE IF EXISTS incidents;

ALTER TABLE checks
  DROP COLUMN IF EXISTS mute_until,
  DROP COLUMN IF EXISTS recover_after_ok,
  DROP COLUMN IF EXISTS fail_after_sec;
