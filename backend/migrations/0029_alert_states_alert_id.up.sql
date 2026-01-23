ALTER TABLE alert_states
  ADD COLUMN IF NOT EXISTS alert_id uuid;

UPDATE alert_states
SET alert_id = gen_random_uuid()
WHERE alert_id IS NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_indexes WHERE indexname = 'alert_states_alert_id_idx'
  ) THEN
    CREATE UNIQUE INDEX alert_states_alert_id_idx ON alert_states(alert_id);
  END IF;
END $$;
