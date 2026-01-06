ALTER TABLE IF EXISTS webauthn_challenges
  ADD COLUMN IF NOT EXISTS options_data jsonb NOT NULL DEFAULT '{}'::jsonb;
