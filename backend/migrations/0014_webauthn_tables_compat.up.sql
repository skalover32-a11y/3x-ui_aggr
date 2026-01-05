CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id text NOT NULL,
    credential_id text NOT NULL UNIQUE,
    public_key bytea NOT NULL,
    sign_count bigint NOT NULL DEFAULT 0,
    transports text[] NOT NULL DEFAULT '{}',
    aaguid text,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_user ON webauthn_credentials(user_id);

CREATE TABLE IF NOT EXISTS webauthn_challenges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id text NOT NULL,
    type text NOT NULL,
    challenge text NOT NULL,
    session_data jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webauthn_challenges_user ON webauthn_challenges(user_id, type);
CREATE INDEX IF NOT EXISTS idx_webauthn_challenges_expires ON webauthn_challenges(expires_at);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'web_authn_credentials'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'webauthn_credentials'
    ) THEN
        ALTER TABLE web_authn_credentials RENAME TO webauthn_credentials;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'web_authn_challenges'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'webauthn_challenges'
    ) THEN
        ALTER TABLE web_authn_challenges RENAME TO webauthn_challenges;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'web_authn_credentials'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.views
        WHERE table_schema = 'public' AND table_name = 'web_authn_credentials'
    ) THEN
        CREATE VIEW web_authn_credentials AS
        SELECT * FROM webauthn_credentials;
        COMMENT ON VIEW web_authn_credentials IS 'read-only compatibility view; write to webauthn_credentials';
    ELSIF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'web_authn_credentials'
    ) AND EXISTS (
        SELECT 1 FROM information_schema.views
        WHERE table_schema = 'public' AND table_name = 'web_authn_credentials'
    ) THEN
        COMMENT ON VIEW web_authn_credentials IS 'read-only compatibility view; write to webauthn_credentials';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'web_authn_challenges'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.views
        WHERE table_schema = 'public' AND table_name = 'web_authn_challenges'
    ) THEN
        CREATE VIEW web_authn_challenges AS
        SELECT * FROM webauthn_challenges;
        COMMENT ON VIEW web_authn_challenges IS 'read-only compatibility view; write to webauthn_challenges';
    ELSIF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'web_authn_challenges'
    ) AND EXISTS (
        SELECT 1 FROM information_schema.views
        WHERE table_schema = 'public' AND table_name = 'web_authn_challenges'
    ) THEN
        COMMENT ON VIEW web_authn_challenges IS 'read-only compatibility view; write to webauthn_challenges';
    END IF;
END $$;
