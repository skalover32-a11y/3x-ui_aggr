CREATE TABLE IF NOT EXISTS auth_refresh_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    user_agent text,
    ip text,
    device_name text
);

CREATE INDEX IF NOT EXISTS idx_auth_refresh_tokens_user ON auth_refresh_tokens(user_id, expires_at);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'refresh_tokens'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'auth_refresh_tokens'
    ) THEN
        ALTER TABLE refresh_tokens RENAME TO auth_refresh_tokens;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'refresh_tokens'
    ) THEN
        CREATE OR REPLACE VIEW refresh_tokens AS
        SELECT * FROM auth_refresh_tokens;
    END IF;
END $$;
