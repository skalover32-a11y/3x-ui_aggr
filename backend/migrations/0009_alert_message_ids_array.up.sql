ALTER TABLE alert_states
    ALTER COLUMN last_message_ids SET DEFAULT '[]'::jsonb;

UPDATE alert_states
SET last_message_ids = '[]'::jsonb
WHERE jsonb_typeof(last_message_ids) <> 'array';
