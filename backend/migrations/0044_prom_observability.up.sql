CREATE TABLE IF NOT EXISTS prom_settings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  mode text NOT NULL DEFAULT 'embedded',
  prom_url text NOT NULL DEFAULT '',
  reload_method text NOT NULL DEFAULT 'http',
  prom_container_name text NOT NULL DEFAULT 'prometheus',
  default_scheme text NOT NULL DEFAULT 'http',
  default_metrics_path text NOT NULL DEFAULT '/metrics',
  default_interval text NOT NULL DEFAULT '15s',
  default_timeout text NOT NULL DEFAULT '5s',
  default_labels jsonb NOT NULL DEFAULT '{}'::jsonb,
  allow_external_reload boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT uq_prom_settings_org UNIQUE (org_id),
  CONSTRAINT chk_prom_settings_mode CHECK (mode IN ('embedded', 'external')),
  CONSTRAINT chk_prom_settings_reload_method CHECK (reload_method IN ('http', 'docker_hup', 'manual')),
  CONSTRAINT chk_prom_settings_scheme CHECK (default_scheme IN ('http', 'https'))
);

CREATE TABLE IF NOT EXISTS prom_targets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name text NOT NULL,
  scheme text NOT NULL DEFAULT 'http',
  address text NOT NULL,
  metrics_path text NOT NULL DEFAULT '/metrics',
  interval text NOT NULL DEFAULT '15s',
  timeout text NOT NULL DEFAULT '5s',
  labels jsonb NOT NULL DEFAULT '{}'::jsonb,
  enabled boolean NOT NULL DEFAULT true,
  auth_type text NOT NULL DEFAULT 'none',
  auth_username text NOT NULL DEFAULT '',
  auth_password_enc text NOT NULL DEFAULT '',
  auth_bearer_token_enc text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT chk_prom_targets_scheme CHECK (scheme IN ('http', 'https')),
  CONSTRAINT chk_prom_targets_auth_type CHECK (auth_type IN ('none', 'basic', 'bearer'))
);

CREATE INDEX IF NOT EXISTS idx_prom_targets_org_id ON prom_targets(org_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_prom_targets_org_name ON prom_targets(org_id, lower(name));
