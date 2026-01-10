ALTER TABLE nodes
  ADD COLUMN IF NOT EXISTS agent_installed boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS agent_last_seen_at timestamptz,
  ADD COLUMN IF NOT EXISTS agent_version text;

UPDATE nodes
SET agent_installed = true
WHERE agent_enabled = true AND agent_installed = false;
