CREATE TABLE IF NOT EXISTS org_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    filename text NOT NULL,
    ext text NOT NULL,
    content_enc text NOT NULL,
    size_bytes integer NOT NULL DEFAULT 0,
    created_by_user_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_org_keys_org_id ON org_keys(org_id);
CREATE INDEX IF NOT EXISTS idx_org_keys_created_at ON org_keys(created_at DESC);
