ALTER TABLE users
    DROP COLUMN IF EXISTS totp_secret_enc,
    DROP COLUMN IF EXISTS totp_enabled,
    DROP COLUMN IF EXISTS recovery_code_hash,
    DROP COLUMN IF EXISTS recovery_code_expires_at;
