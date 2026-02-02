ALTER TABLE org_keys
    DROP COLUMN IF EXISTS node_id,
    DROP COLUMN IF EXISTS fingerprint,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS label;
