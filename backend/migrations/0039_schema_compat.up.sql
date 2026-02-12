ALTER TABLE node_metrics_latest
  ADD COLUMN IF NOT EXISTS service_version text NULL,
  ADD COLUMN IF NOT EXISTS service_running boolean NULL,
  ADD COLUMN IF NOT EXISTS panel_version text NULL,
  ADD COLUMN IF NOT EXISTS panel_running boolean NULL,
  ADD COLUMN IF NOT EXISTS runtime_running boolean NULL;

ALTER TABLE telegram_settings
  ADD COLUMN IF NOT EXISTS org_id uuid NULL REFERENCES organizations(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_telegram_settings_org_id ON telegram_settings(org_id);

-- Backfill org-scoped settings when only global row existed.
UPDATE telegram_settings
SET org_id = (
  SELECT id FROM organizations WHERE name = 'VLF Root' ORDER BY created_at LIMIT 1
)
WHERE org_id IS NULL;
