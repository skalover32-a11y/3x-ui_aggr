-- Ensure org_role enum exists
DO $$
BEGIN
  CREATE TYPE org_role AS ENUM ('owner', 'admin', 'viewer');
EXCEPTION
  WHEN duplicate_object THEN NULL;
END $$;

-- Ensure invites table exists (older deployments may already have it)
CREATE TABLE IF NOT EXISTS invites (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code text UNIQUE NOT NULL,
  created_by_user_id uuid NULL,
  target_org_id uuid NULL REFERENCES organizations(id) ON DELETE CASCADE,
  mode text NOT NULL DEFAULT 'create_private_stack',
  role org_role NOT NULL DEFAULT 'owner',
  org_name text NULL,
  expires_at timestamptz NOT NULL,
  used_at timestamptz NULL,
  used_by_user_id uuid NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE invites ADD COLUMN IF NOT EXISTS created_by_user_id uuid NULL;
ALTER TABLE invites ADD COLUMN IF NOT EXISTS target_org_id uuid NULL REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE invites ADD COLUMN IF NOT EXISTS mode text NOT NULL DEFAULT 'create_private_stack';
ALTER TABLE invites ADD COLUMN IF NOT EXISTS role org_role NOT NULL DEFAULT 'owner';
ALTER TABLE invites ADD COLUMN IF NOT EXISTS org_name text NULL;
ALTER TABLE invites ADD COLUMN IF NOT EXISTS expires_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE invites ADD COLUMN IF NOT EXISTS used_at timestamptz NULL;
ALTER TABLE invites ADD COLUMN IF NOT EXISTS used_by_user_id uuid NULL;
ALTER TABLE invites ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();

-- Backfill missing creator if needed
UPDATE invites
SET created_by_user_id = COALESCE(
  created_by_user_id,
  (SELECT owner_user_id FROM organizations WHERE name = 'VLF Root' LIMIT 1),
  (SELECT id FROM users ORDER BY created_at LIMIT 1)
)
WHERE created_by_user_id IS NULL;

-- Indexes
CREATE UNIQUE INDEX IF NOT EXISTS invites_code_key ON invites(code);
CREATE INDEX IF NOT EXISTS idx_invites_expires ON invites(expires_at);
CREATE INDEX IF NOT EXISTS idx_invites_used ON invites(used_at);
CREATE INDEX IF NOT EXISTS idx_invites_target_org ON invites(target_org_id);
