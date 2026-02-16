ALTER TABLE telegram_settings
  ADD COLUMN IF NOT EXISTS ack_mute_minutes integer NOT NULL DEFAULT 1440,
  ADD COLUMN IF NOT EXISTS mute_minutes integer NOT NULL DEFAULT 60;

UPDATE telegram_settings
SET
  ack_mute_minutes = CASE WHEN ack_mute_minutes <= 0 THEN 1440 ELSE ack_mute_minutes END,
  mute_minutes = CASE WHEN mute_minutes <= 0 THEN 60 ELSE mute_minutes END;
