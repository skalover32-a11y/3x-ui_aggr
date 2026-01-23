DROP INDEX IF EXISTS alert_states_alert_id_idx;
ALTER TABLE alert_states
  DROP COLUMN IF EXISTS alert_id;
