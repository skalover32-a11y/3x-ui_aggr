-- 0034_invites_signup.up.sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS invites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE,
    created_by_user_id uuid NULL,
    role text NOT NULL DEFAULT 'owner',
    org_name text NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz NULL,
    used_by_user_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_invites_expires ON invites(expires_at);
CREATE INDEX IF NOT EXISTS idx_invites_used ON invites(used_at);
