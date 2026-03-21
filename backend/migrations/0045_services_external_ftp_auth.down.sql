DELETE FROM services
WHERE node_id IS NULL;

DROP INDEX IF EXISTS idx_services_org_id;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_services_org_id'
    ) THEN
        ALTER TABLE services DROP CONSTRAINT fk_services_org_id;
    END IF;
END $$;

ALTER TABLE services
    ALTER COLUMN node_id SET NOT NULL;

ALTER TABLE services
    DROP COLUMN IF EXISTS auth_password_enc,
    DROP COLUMN IF EXISTS auth_username,
    DROP COLUMN IF EXISTS name,
    DROP COLUMN IF EXISTS org_id;
