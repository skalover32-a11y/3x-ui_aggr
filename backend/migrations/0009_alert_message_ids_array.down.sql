ALTER TABLE alert_states
    ALTER COLUMN last_message_ids SET DEFAULT '{}'::jsonb;
