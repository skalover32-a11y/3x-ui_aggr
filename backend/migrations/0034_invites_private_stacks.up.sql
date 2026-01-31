CREATE TABLE IF NOT EXISTS invites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text UNIQUE NOT NULL,
    created_by_user_id uuid NOT NULL,
    target_org_id uuid NULL REFERENCES organizations(id) ON DELETE CASCADE,
    mode text NOT NULL,
    role org_role NOT NULL DEFAULT 'owner',
    org_name text NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz NULL,
    used_by_user_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_invites_code ON invites (code);
CREATE INDEX IF NOT EXISTS idx_invites_expires ON invites (expires_at);
CREATE INDEX IF NOT EXISTS idx_invites_target_org ON invites (target_org_id);
