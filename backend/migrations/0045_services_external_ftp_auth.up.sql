ALTER TABLE services
    ADD COLUMN IF NOT EXISTS org_id uuid,
    ADD COLUMN IF NOT EXISTS name text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS auth_username text,
    ADD COLUMN IF NOT EXISTS auth_password_enc text;

UPDATE services s
SET org_id = n.org_id
FROM nodes n
WHERE s.node_id = n.id
  AND s.org_id IS NULL;

UPDATE services
SET name = CASE
    WHEN btrim(coalesce(name, '')) <> '' THEN btrim(name)
    WHEN btrim(coalesce(host, '')) <> '' THEN btrim(host)
    WHEN btrim(coalesce(url, '')) <> '' THEN btrim(url)
    ELSE lower(coalesce(kind, 'service')) || '-' || substr(id::text, 1, 8)
END
WHERE btrim(coalesce(name, '')) = '';

ALTER TABLE services
    ALTER COLUMN org_id SET NOT NULL,
    ALTER COLUMN node_id DROP NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_services_org_id'
    ) THEN
        ALTER TABLE services
            ADD CONSTRAINT fk_services_org_id
            FOREIGN KEY (org_id) REFERENCES organizations(id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_services_org_id ON services(org_id);
