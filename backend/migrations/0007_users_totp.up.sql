ALTER TABLE users
    ADD COLUMN IF NOT EXISTS totp_secret_enc text,
    ADD COLUMN IF NOT EXISTS totp_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS recovery_code_hash text,
    ADD COLUMN IF NOT EXISTS recovery_code_expires_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_users_totp_enabled ON users(totp_enabled);
