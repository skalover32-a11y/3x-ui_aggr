ALTER TABLE org_keys
    ADD COLUMN IF NOT EXISTS label text NULL,
    ADD COLUMN IF NOT EXISTS description text NULL,
    ADD COLUMN IF NOT EXISTS fingerprint text NULL,
    ADD COLUMN IF NOT EXISTS node_id uuid NULL REFERENCES nodes(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_org_keys_node_id ON org_keys(node_id);
