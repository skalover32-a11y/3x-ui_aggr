ALTER TABLE telegram_settings
  DROP COLUMN IF EXISTS ack_mute_minutes,
  DROP COLUMN IF EXISTS mute_minutes;
