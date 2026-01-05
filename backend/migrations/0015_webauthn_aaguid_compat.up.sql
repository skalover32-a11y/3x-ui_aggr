DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'webauthn_credentials' AND column_name = 'aaguid'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'webauthn_credentials' AND column_name = 'aa_guid'
    ) THEN
        ALTER TABLE webauthn_credentials ADD COLUMN aa_guid text;
        UPDATE webauthn_credentials SET aa_guid = aaguid WHERE aaguid IS NOT NULL;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'webauthn_credentials' AND column_name = 'aa_guid'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'webauthn_credentials' AND column_name = 'aaguid'
    ) THEN
        ALTER TABLE webauthn_credentials ADD COLUMN aaguid text;
        UPDATE webauthn_credentials SET aaguid = aa_guid WHERE aa_guid IS NOT NULL;
    END IF;
END $$;
