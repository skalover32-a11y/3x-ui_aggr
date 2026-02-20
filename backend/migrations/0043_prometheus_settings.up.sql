CREATE TABLE IF NOT EXISTS prometheus_settings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  enabled boolean NOT NULL DEFAULT false,
  base_url text NOT NULL DEFAULT '',
  auth_type text NOT NULL DEFAULT 'none',
  username text NOT NULL DEFAULT '',
  password_enc text NOT NULL DEFAULT '',
  bearer_token_enc text NOT NULL DEFAULT '',
  tls_insecure_skip_verify boolean NOT NULL DEFAULT false,
  timeout_ms integer NOT NULL DEFAULT 5000,
  default_step_sec integer NOT NULL DEFAULT 60,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT chk_prometheus_auth_type CHECK (auth_type IN ('none', 'basic', 'bearer'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_prometheus_settings_org_id ON prometheus_settings(org_id);
