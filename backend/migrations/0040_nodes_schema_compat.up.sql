-- Compatibility migration for mixed historical schemas.
-- Safe to run multiple times.

ALTER TABLE nodes
  ADD COLUMN IF NOT EXISTS runtime_version text NULL,
  ADD COLUMN IF NOT EXISTS service_version text NULL,
  ADD COLUMN IF NOT EXISTS panel_version text NULL,
  ADD COLUMN IF NOT EXISTS versions_checked_at timestamptz NULL;

ALTER TABLE node_metrics_latest
  ADD COLUMN IF NOT EXISTS runtime_running boolean NULL,
  ADD COLUMN IF NOT EXISTS service_version text NULL,
  ADD COLUMN IF NOT EXISTS service_running boolean NULL,
  ADD COLUMN IF NOT EXISTS panel_version text NULL,
  ADD COLUMN IF NOT EXISTS panel_running boolean NULL;

ALTER TABLE telegram_settings
  ADD COLUMN IF NOT EXISTS org_id uuid NULL REFERENCES organizations(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_telegram_settings_org_id ON telegram_settings(org_id);

-- Backfill aliases where only service_* existed before.
UPDATE nodes
SET panel_version = service_version
WHERE panel_version IS NULL
  AND service_version IS NOT NULL;

UPDATE node_metrics_latest
SET panel_version = service_version
WHERE panel_version IS NULL
  AND service_version IS NOT NULL;

UPDATE node_metrics_latest
SET panel_running = service_running
WHERE panel_running IS NULL
  AND service_running IS NOT NULL;

-- Backfill org scope for legacy telegram settings row.
UPDATE telegram_settings
SET org_id = (
  SELECT id FROM organizations WHERE name = 'VLF Root' ORDER BY created_at LIMIT 1
)
WHERE org_id IS NULL;
