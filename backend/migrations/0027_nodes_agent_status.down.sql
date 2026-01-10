ALTER TABLE nodes
  DROP COLUMN IF EXISTS agent_version,
  DROP COLUMN IF EXISTS agent_last_seen_at,
  DROP COLUMN IF EXISTS agent_installed;
